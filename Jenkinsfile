// board CI on the homelab Jenkins: multibranch job board/ci (see docs/ci.md).
//
// Distributed pipeline: every test stage is a "unit" that runs as its own
// parallel branch with its own retry policy and timing, instead of one stage
// after another. Every unit runs on int21h-ws
// (amd64, 12 hardware threads, 4 executors): the arm64 phones of the board-test pool
// were too slow (#16 took 5 h, mutation lanes ran out of 105 minutes and
// agents dropped under load 16-53), so no unit asks for them any more. Units
// that find all 4 executors busy queue for one.
//
// Retry policy, per unit (on the same node: there is only one):
//   - infrastructure failure (agent lost, Docker unavailable, checkout or CI
//     image build failed, the attempt timed out or went silent, no executor
//     free): another attempt, up to 3 infrastructure failures;
//   - the unit's command fails: one rerun; a pass there marks the build
//     UNSTABLE ("flaky"), a second failure fails it.
// A unit may cap its attempts (maxAttempts: the full mutation run has one).
// Each attempt reports node, queue wait and run time; the build page gets a
// per-unit timing table (ci-results/distributed/timing.md).
//
// Each unit checks out the build's commit in its own workspace, builds
// board-ci:local (ci/jenkins-image.sh; cached by the Docker daemon), runs,
// removes the sibling containers it labelled (ralphd.run=<attempt id>, by
// exact ID) and stashes its ci-results. Quality waits for Suite (coverage)
// and Image (the image tar). The report runs once at the end over every
// unit's stash.
//
// Three phases, one after another; each phase's units run in parallel:
//   1. Core (every build): Lint, Suite, Image, Allure smoke, then Quality
//      (4 executors: Quality waits for Suite and Image without holding one).
//   2. Stability (EXTENDED=true, or nightly on main): three stability runs,
//      one unit each, then the Perf unit (the SPEC 14 harness, 30 minutes,
//      alone on the agent); EXTENDED also adds -race and fresh vulnerability
//      databases to Core.
//   3. Mutation, one unit, in one of two modes:
//      - full (nightly on main, or MUTATION_FULL=true): every package and
//        Stryker (ci/mutation.sh lane all, one package after another), each
//        gremlins with nproc/2 workers and GOMAXPROCS 2 per worker (a mutant
//        is a go build plus a go test; 6 x 2 keeps the 12 threads busy
//        without oversubscribing them, and nice 10 leaves other jobs ahead
//        of it). One attempt under one 6-hour limit: a timeout is not worth
//        another 6 hours. About 70 minutes.
//      - diff (MUTATION=true on any other build): only the lines changed
//        since a base, under 45 minutes (minutes when few packages changed;
//        a changed internal/server still pays its covered suite run once).
//        A branch or PR build diffs against the merge base with its target
//        (origin/main, or CHANGE_TARGET), fetched into the workspace first
//        because the multibranch checkout fetches the built branch only. A
//        build of main itself would get HEAD as that merge base, so it diffs
//        against HEAD~1 (the last commit; the nightly full run covers the
//        rest). ci/mutation.sh and ci/mutationgate -diff: a package with no
//        mutant passes, one with mutants keeps the 80 efficacy floor;
//        Stryker runs only if web/src/lib changed.
// A phase runs only if every earlier one passed (UNSTABLE counts as passed).
@Library('homelab') _
import groovy.transform.Field

// Units' results and attempts, shared by the parallel branches.
@Field def results = [:]        // unit -> 'pass' | 'flaky' | 'fail'
@Field def attemptLog = [:]     // unit -> [attempt maps]
@Field def signals = [:]        // 'Suite' / 'Image' -> true once stashed (pass or fail)
@Field String commitSha = ''
@Field String imageVersion = ''
@Field String mutationBase = ''  // diff mode: the commit the mutation diff starts from
@Field final String AMD = 'board-test && amd64 && docker'

long nowMs() { System.currentTimeMillis() }

String slug(String s) { s.toLowerCase().replaceAll('[^a-z0-9]+', '-') }

// What ended an attempt that threw. Returns null for an abort of the whole
// build, which must propagate; otherwise an 'infra:<why>' outcome.
String classify(Throwable e, Map a) {
  if (e instanceof org.jenkinsci.plugins.workflow.steps.FlowInterruptedException) {
    // getCauses needs a script approval; without it, treat as an abort.
    String causes = ''
    try {
      causes = e.causes.collect { it.toString() }.join(' ')
    } catch (Throwable ignored) {
      causes = 'unknown'
    }
    if (causes.contains('ExceededTimeout') || causes.contains('Timeout')) {
      return a.node ? 'infra:timeout' : 'infra:no-node'
    }
    if (causes.contains('RemovedNode') || causes.contains('Offline') || causes.contains('QueueTaskCancelled')) {
      return 'infra:agent-lost'
    }
    if (causes.contains('UserInterruption') || causes == 'unknown') {
      return null
    }
    return 'infra:interrupted'
  }
  // A pipeline-code error (serialization, sandbox) fails the same on any
  // node: never worth another node.
  if (e instanceof java.io.NotSerializableException ||
      e.toString().contains('RejectedAccessException') ||
      e instanceof groovy.lang.MissingPropertyException ||
      e instanceof groovy.lang.MissingMethodException) {
    return null
  }
  return 'infra:' + e.toString().take(80)
}

// Runs one unit on int21h-ws (AMD; every attempt asks for the same label,
// which matches that one node). u: name, minutes (the attempt's hard limit),
// body (closure on the node, returns the command's exit status), needs
// (stash names to unstash first), extraStash (closure on the node, after a
// run), maxAttempts (default 4).
def runUnit(Map u) {
  def attempts = []
  attemptLog[u.name] = attempts
  int infra = 0
  boolean rerun = false
  String firstFail = null
  int maxAttempts = u.maxAttempts ?: 4
  while (true) {
    def a = [n: attempts.size() + 1, label: AMD, node: null, queued: nowMs(), start: 0L, end: 0L, outcome: null]
    attempts << a
    String runId = "jenkins-${env.BUILD_NUMBER}-${slug(u.name)}-${a.n}"
    try {
      // Queue wait (no free executor) is capped too: 45 minutes.
      timeout(time: 45 + u.minutes, unit: 'MINUTES') {
        node(AMD) {
          a.node = env.NODE_NAME
          a.start = nowMs()
          echo "unit ${u.name}: attempt ${a.n} of at most ${maxAttempts} on ${env.NODE_NAME}"
          try {
            // One hard limit per attempt, no inner activity timeout: when the
            // hard limit fired first, the orphaned activity timer later found
            // no thread and Jenkins force-killed the whole build (#12).
            timeout(time: u.minutes, unit: 'MINUTES') {
              if (sh(returnStatus: true, script: 'docker info >/dev/null 2>&1') != 0) {
                a.outcome = 'infra:docker'
                return
              }
              checkout scm
              sh 'git clean -ffdxq'
              if (!commitSha) {
                commitSha = sh(returnStdout: true, script: 'git rev-parse HEAD').trim()
                imageVersion = sh(returnStdout: true, script: 'git describe --tags --exact-match HEAD 2>/dev/null || git rev-parse --short=7 HEAD').trim()
              }
              if (sh(returnStatus: true, script: 'ci/jenkins-image.sh') != 0) {
                a.outcome = 'infra:ci-image'
                return
              }
              (u.needs ?: []).each { unstash it }
              withEnv(["RALPHD_RUN_ID=${runId}", "BOARD_CI_SHA=${commitSha}",
                       "BOARD_IMAGE_REVISION=${commitSha}", "BOARD_IMAGE_VERSION=${imageVersion}"]) {
                int rc = u.body()
                // -1: the durable-task wrapper died (agent restart killed its
                // process group), so the command never finished.
                a.outcome = rc == 0 ? 'pass' : rc == -1 ? 'infra:process-lost' : "fail:${rc}"
              }
              stash name: 'res-' + slug(u.name), includes: 'ci-results/**',
                    excludes: 'ci-results/board-image.tar,ci-results/board-image-oci.tar', allowEmpty: true
              if (u.extraStash) {
                u.extraStash()
              }
            }
          } finally {
            // Sibling and Testcontainers containers of this attempt, by exact
            // ID. Bounded: on a lost agent this would wait for it forever.
            try {
              timeout(time: 3, unit: 'MINUTES') {
                sh(returnStatus: true, script: """
                  ids=\$(docker ps -aq --filter label=ralphd.run=${runId})
                  [ -z "\$ids" ] || { echo "removing leftover containers: \$ids"; docker rm -f \$ids; }
                """)
              }
            } catch (Throwable c) {
              echo "unit ${u.name}: container cleanup on ${a.node} skipped: ${c}"
            }
          }
        }
      }
    } catch (Throwable e) {
      String why = classify(e, a)
      if (why == null) {
        a.outcome = 'aborted'
        a.end = nowMs()
        throw e
      }
      a.outcome = why
      echo "unit ${u.name}: attempt ${a.n} on ${a.node ?: '(none)'} failed: ${why} (${e})"
    }
    a.end = nowMs()
    if (a.outcome == 'pass') {
      if (firstFail) {
        results[u.name] = 'flaky'
        unstable("${u.name}: flaky on ${firstFail} (passed on ${a.node})")
      } else {
        results[u.name] = 'pass'
      }
      return
    }
    if (a.outcome.startsWith('fail')) {
      if (!rerun && attempts.size() < maxAttempts) {
        rerun = true
        firstFail = a.node
        echo "unit ${u.name}: failed on ${a.node}; one rerun"
        continue
      }
      results[u.name] = 'fail'
      error("${u.name}: failed" + (firstFail ? ' twice' : '') + ' (' + attemptSummary(attempts) + ')')
    }
    infra++
    if (infra >= 3 || attempts.size() >= maxAttempts) {
      results[u.name] = 'fail'
      error("${u.name}: gave up after ${attempts.size()} attempt" + (attempts.size() == 1 ? '' : 's') + ' (' + attemptSummary(attempts) + ')')
    }
  }
}

// "3 infrastructure failures, 1 test failure: int21h-ws infra:timeout, ..."
@NonCPS
String attemptSummary(List attempts) {
  int fails = attempts.count { it.outcome?.startsWith('fail') }
  int infras = attempts.count { it.outcome?.startsWith('infra') }
  def parts = []
  if (infras) {
    parts << "${infras} infrastructure failure" + (infras == 1 ? '' : 's')
  }
  if (fails) {
    parts << "${fails} test failure" + (fails == 1 ? '' : 's')
  }
  return parts.join(', ') + ': ' + attempts.collect { (it.node ?: 'none') + ' ' + it.outcome }.join(', ')
}

// The commit diff-mode mutation starts from (see the header), on the unit's
// node, in its checkout. A failed fetch throws: an infrastructure failure.
String mutationDiffBase() {
  if (env.BRANCH_NAME == 'main') {
    return sh(returnStdout: true, script: 'git rev-parse HEAD~1').trim()
  }
  String target = env.CHANGE_TARGET ?: 'main'
  withCredentials([gitUsernamePassword(credentialsId: 'github-n-orlov', gitToolName: 'Default')]) {
    sh "git fetch --no-tags --quiet origin +refs/heads/${target}:refs/remotes/origin/${target}"
  }
  return sh(returnStdout: true, script: "git merge-base origin/${target} HEAD").trim()
}

// A unit as a parallel branch: its failure fails the stage and the build,
// never the sibling branches.
def unitBranch(Map u) {
  return {
    stage(u.name) {
      catchError(buildResult: 'FAILURE', stageResult: 'FAILURE') {
        try {
          runUnit(u)
        } finally {
          if (u.signal) {
            signals[u.signal] = true
          }
        }
      }
    }
  }
}

@NonCPS
String timingReport() {
  def lines = ['| unit | result | attempt | node | queue (min) | run (min) | outcome |',
               '|------|--------|---------|------|-------------|-----------|---------|']
  attemptLog.keySet().sort().each { name ->
    attemptLog[name].each { a ->
      def q = a.start ? String.format('%.1f', (a.start - a.queued) / 60000.0) : String.format('%.1f', ((a.end ?: nowMs()) - a.queued) / 60000.0)
      def r = a.start ? String.format('%.1f', ((a.end ?: nowMs()) - a.start) / 60000.0) : '-'
      lines << "| ${name} | ${results[name] ?: '?'} | ${a.n} | ${a.node ?: '-'} | ${q} | ${r} | ${a.outcome} |"
    }
  }
  return lines.join('\n') + '\n'
}

pipeline {
  agent none

  options {
    ansiColor('xterm')
    timestamps()
    buildDiscarder(logRotator(numToKeepStr: '30'))
    disableConcurrentBuilds()
    // Core and Stability (about 1 h) plus the full mutation run's 6 hours.
    timeout(time: 480, unit: 'MINUTES')
    copyArtifactPermission('/board/release,/board/ci/*')
  }

  parameters {
    booleanParam(name: 'EXTENDED', defaultValue: false,
                 description: 'Extended CI: -race, fresh vulnerability databases, then the stability runs')
    booleanParam(name: 'MUTATION', defaultValue: false,
                 description: 'Mutation testing in diff mode: only the lines changed since the merge base with main (on main: since HEAD~1), after Core (and Stability if extended)')
    booleanParam(name: 'MUTATION_FULL', defaultValue: false,
                 description: 'Full mutation testing: every package and Stryker in one unit (about 70 minutes, 6-hour limit), as the nightly build of main does')
  }

  triggers {
    cron(env.BRANCH_NAME == 'main' ? '17 3 * * *' : '')
  }

  environment {
    BOARD_CI_OUT = 'ci-results'
    BOARD_CI_CACHE_VOLUME = 'board-go-cache-jenkins'
    BOARD_CI_NPM_VOLUME = 'board-npm-cache-jenkins'
    BOARD_CI_RUN_URL = "${env.BUILD_URL}"
    BOARD_CI_BUILD_NAME = "${env.JOB_NAME} #${env.BUILD_NUMBER}"
  }

  stages {
    stage('Core') {
      steps {
        script {
          boolean timer = !currentBuild.getBuildCauses('hudson.triggers.TimerTrigger$TimerTriggerCause').isEmpty()
          boolean nightly = params.EXTENDED || timer
          env.CI_NIGHTLY = nightly ? 'true' : 'false'
          boolean full = params.MUTATION_FULL || timer
          env.CI_MUTATION = (params.MUTATION || full) ? 'true' : 'false'
          env.CI_MUTATION_MODE = full ? 'full' : 'diff'
          env.BOARD_CI_GO_EXTRA_FLAGS = nightly ? '-race' : ''
          def lanes = ['core'] + (nightly ? ['stability'] : []) + (env.CI_MUTATION == 'true' ? ["mutation(${env.CI_MUTATION_MODE})"] : [])
          currentBuild.description = 'int21h-ws: ' + lanes.join(' + ') + (nightly ? ' (-race)' : '')
          def units = []
          // gofmt, go vet, tidy drift, golangci-lint.
          units << [name: 'Lint', minutes: 30, body: { sh(returnStatus: true, script: 'ci/run.sh ci/lint.sh') }]
          units << [name: 'Suite', minutes: nightly ? 120 : 45, signal: 'Suite',
                    body: { sh(returnStatus: true, script: 'ci/run.sh ci/suite.sh') }]
          // Both platforms cross-built and never pushed; the amd64 image is run
          // and probed.
          units << [name: 'Image', minutes: 30, signal: 'Image',
                    body: {
                      int rc = sh(returnStatus: true, script: 'ci/image-build.sh "$BOARD_CI_OUT/board-image.tar" && ci/image-check.sh "$BOARD_CI_OUT"')
                      if (rc != 0) {
                        return rc
                      }
                      // The release dry run (SPEC 12.5): every asset built at this commit and
                      // checked, nothing published.
                      sh(returnStatus: true, script: 'ci/release-check.sh "$BOARD_CI_OUT"')
                    },
                    extraStash: { stash name: 'image', includes: 'ci-results/board-image.tar,ci-results/board-image-oci.tar', allowEmpty: true }]
          units << [name: 'Allure smoke', minutes: 15,
                    body: {
                      sh(returnStatus: true, script: '''
                        set -e
                        mkdir -p allure-smoke/fixture/allure-results
                        BOARD_ALLURE_FIXTURE_OUT=/w/allure-smoke/fixture/allure-results \
                          ci/run.sh go test -count=1 -run '^TestAllureFormatterWritesResultsForAFixtureRun$' ./features/
                        ALLURE_CACHE_DIR="$HOME/.cache/board-allure" ci/allure-smoke.sh allure-smoke/fixture allure-smoke/report
                      ''')
                    }]
          def branches = [:]
          units.each { u -> branches[u.name] = unitBranch(u) }
          // The quality gates read Suite's coverage and Image's tar: wait for both.
          branches['Quality'] = {
            stage('Quality') {
              waitUntil(initialRecurrencePeriod: 15000) { signals['Suite'] == true && signals['Image'] == true }
              catchError(buildResult: 'FAILURE', stageResult: 'FAILURE') {
                env.QUALITY_RESULT = 'failing'
                runUnit([name: 'Quality', minutes: 30, needs: ['res-suite', 'res-image', 'image'],
                         body: {
                           if (env.CI_NIGHTLY == 'true') {
                             sh "ci/run.sh sh -c 'rm -rf /go-cache/trivy/db /go-cache/trivy/java-db /go-cache/xdg-cache/govulncheck'"
                           }
                           // crap-ts and the openapi gates read web/node_modules, which
                           // Suite installed in its own workspace, not this one.
                           int rc = sh(returnStatus: true, script: "ci/run.sh sh -c '[ -d web/node_modules ] || (cd web && npm ci --no-audit --no-fund)'")
                           if (rc != 0) {
                             return rc
                           }
                           sh(returnStatus: true, script: 'ci/run.sh ci/quality.sh "$BOARD_CI_OUT" ci/quality.json --mutation-deferred')
                         }])
                env.QUALITY_RESULT = results['Quality'] == 'fail' ? 'failing' : 'passing'
              }
            }
          }
          parallel branches
        }
      }
    }

    stage('Stability') {
      when {
        beforeAgent true
        expression { env.CI_NIGHTLY == 'true' && currentBuild.currentResult != 'FAILURE' }
      }
      steps {
        script {
          def branches = [:]
          // A list, not 1..3: an IntRange is not serializable in CPS.
          [1, 2, 3].each { i ->
            // stability.sh prints only at the end (11 min on int21h-ws alone).
            def u = [name: "Stability ${i}", minutes: 40,
                     body: {
                       sh(returnStatus: true, script: '''
                         set -e
                         mkdir -p "$BOARD_CI_OUT/stability"
                         TMPDIR="$PWD/$BOARD_CI_OUT/stability" ci/stability.sh 1
                       ''')
                     }]
            branches[u.name] = unitBranch(u)
          }
          parallel branches
          // Perf (SPEC 14, R14): the harness against the real binary, 30
          // minutes (about 4 on int21h-ws). It runs after the three stability
          // units, alone on the agent: its p99 and RSS gates measure the
          // product, not three suites running beside it. Its failure fails
          // the build like any unit's; perf.json is archived with the rest.
          def perf = [name: 'Perf', minutes: 30,
                      body: { sh(returnStatus: true, script: 'ci/run.sh ci/perf.sh "$BOARD_CI_OUT"') }]
          unitBranch(perf).call()
        }
      }
    }

    stage('Mutation') {
      when {
        beforeAgent true
        expression { env.CI_MUTATION == 'true' && currentBuild.currentResult != 'FAILURE' }
      }
      steps {
        script {
          // One unit in either mode (see the header). The worker and CPU
          // split is read by ci/mutation.sh before it scrubs BOARD_*, and
          // ci/run.sh forwards only BOARD_* into its container.
          String tune = 'BOARD_MUTATION_WORKERS=$(( $(nproc) / 2 )) BOARD_MUTATION_TEST_CPU=2'
          def u
          if (env.CI_MUTATION_MODE == 'full') {
            u = [name: 'Mutation full', minutes: 360, maxAttempts: 1,
                 body: { sh(returnStatus: true, script: tune + ' ci/run.sh ci/mutation.sh ci-results/mutation ci/quality.json all') }]
          } else {
            u = [name: 'Mutation diff', minutes: 45,
                 body: {
                   mutationBase = mutationDiffBase()
                   currentBuild.description = currentBuild.description.replaceAll('mutation\\(diff[^)]*\\)', "mutation(diff ${mutationBase.take(7)})")
                   sh(returnStatus: true, script: "BOARD_MUTATION_DIFF=${mutationBase} " + tune +
                                                  ' ci/run.sh ci/mutation.sh ci-results/mutation ci/quality.json all')
                 }]
          }
          stage(u.name) {
            catchError(buildResult: 'FAILURE', stageResult: 'FAILURE') {
              runUnit(u)
            }
          }
        }
      }
    }
  }

  post {
    always {
      script {
        // The report on one pool node over every unit's stash.
        try {
          timeout(time: 30, unit: 'MINUTES') {
            node(AMD) {
              sh 'rm -rf ci-results ci-summary allure-report previous'
              // Plain lists: a map's key view is not serializable across steps.
              for (String name : attemptLog.collect { it.key }) {
                try {
                  unstash 'res-' + slug(name)
                } catch (Throwable e) {
                  echo "no results stashed for ${name}"
                }
              }
              writeFile file: 'ci-results/distributed/timing.md', text: timingReport()
              echo timingReport()
              junit testResults: 'ci-results/junit-merged/*.xml', allowEmptyResults: true,
                    skipMarkingBuildUnstable: true, skipPublishingChecks: true
              copyArtifacts projectName: env.JOB_NAME, selector: lastCompleted(), filter: 'allure-history/**',
                            target: 'previous', optional: true, fingerprintArtifacts: false
              catchError(buildResult: 'UNSTABLE', stageResult: 'FAILURE') {
                sh 'ALLURE_CACHE_DIR="$HOME/.cache/board-allure" ci/jenkins-report.sh "$BOARD_CI_OUT" previous/allure-history'
              }
              publishHTML([reportName: 'Allure', reportDir: 'allure-report', reportFiles: 'index.html',
                           keepAll: true, alwaysLinkToLastBuild: true, allowMissing: true])
              publishHTML([reportName: 'CI summary', reportDir: 'ci-summary', reportFiles: 'index.html',
                           keepAll: true, alwaysLinkToLastBuild: true, allowMissing: true])
              def testsBadge = addEmbeddableBadgeConfiguration(id: 'tests', subject: 'tests', status: 'not run', color: 'lightgrey')
              def coverageBadge = addEmbeddableBadgeConfiguration(id: 'coverage', subject: 'coverage', status: 'not run', color: 'lightgrey')
              def qualityBadge = addEmbeddableBadgeConfiguration(id: 'quality', subject: 'quality', status: 'not run', color: 'lightgrey')
              if (env.QUALITY_RESULT) {
                qualityBadge.setStatus(env.QUALITY_RESULT)
                qualityBadge.setColor(env.QUALITY_RESULT == 'passing' ? 'brightgreen' : 'red')
              }
              if (fileExists('ci-summary/counts.txt')) {
                def c = [:]
                readFile('ci-summary/counts.txt').split('\n').each { line ->
                  def i = line.indexOf('=')
                  if (i > 0) {
                    c[line.substring(0, i)] = line.substring(i + 1).trim()
                  }
                }
                if (c.pass) {
                  def text = 'Tests: ' + c.pass + ' passed, ' + c.fail + ' failed, ' + c.skip + ' skipped, ' + c.flaky + ' flaky'
                  if (c.coverage) {
                    text += '; unit coverage ' + c.coverage
                  }
                  def ok = c.fail == '0'
                  testsBadge.setStatus(ok ? c.pass + ' passed' : c.fail + ' failed')
                  testsBadge.setColor(ok ? 'brightgreen' : 'red')
                  if (c.coverage) {
                    def pct = c.coverage.replace('%', '').toDouble()
                    coverageBadge.setStatus(c.coverage)
                    coverageBadge.setColor(pct >= 80 ? 'brightgreen' : pct >= 60 ? 'yellow' : 'red')
                  }
                  addSummary(icon: ok ? 'symbol-checkmark-circle-outline plugin-ionicons-api'
                                      : 'symbol-close-circle-outline plugin-ionicons-api',
                             text: text + ' (<a href="' + env.BUILD_URL + 'Allure/">Allure</a>, ' +
                                   '<a href="' + env.BUILD_URL + 'CI_20summary/">CI summary</a>)')
                }
              }
              def flaky = results.findAll { it.value == 'flaky' }.collect { it.key }
              addSummary(icon: 'symbol-time-outline plugin-ionicons-api',
                         text: 'Distributed: ' + attemptLog.size() + ' units on ' +
                               attemptLog.values().flatten().collect { it.node }.findAll { it }.unique().sort().join(', ') +
                               (flaky ? '; flaky: ' + flaky.join(', ') : '') +
                               ' (<a href="' + env.BUILD_URL + 'artifact/ci-results/distributed/timing.md">timing</a>)')
              archiveArtifacts artifacts: 'ci-results/**/*.txt, ci-results/**/*.xml, ci-results/**/*.out, ci-results/*.cdx.json, ci-results/mutation/*.json, ci-results/stability/**/*.log, ci-results/perf.json, ci-results/distributed/**, ci-summary/**, allure-history/**',
                               allowEmptyArchive: true
              if (currentBuild.currentResult == 'SUCCESS' && env.BRANCH_NAME == 'main') {
                writeFile file: 'ci-results/release-candidate.json',
                          text: '{"commit": "' + commitSha + '", "build_url": "' + env.BUILD_URL +
                                '", "build_number": ' + env.BUILD_NUMBER + '}\n'
                archiveArtifacts artifacts: 'ci-results/release-candidate.json', fingerprint: true
              }
            }
          }
        } catch (Throwable e) {
          echo "report failed: ${e}"
          echo timingReport()
        }
      }
    }
    cleanup {
      script {
        def watched = env.BRANCH_NAME == 'main' || env.CI_NIGHTLY == 'true' || env.CI_MUTATION == 'true'
        // agent none: notifyTelegram's sh needs a workspace.
        try {
          timeout(time: 10, unit: 'MINUTES') {
            node(AMD) {
              notifyTelegram(notifyOn: 'ALL', silent: watched ? null : true,
                             extra: 'distributed' + (env.CI_NIGHTLY == 'true' ? ', extended lane' : '') +
                                    (env.CI_MUTATION == 'true' ? ", mutation(${env.CI_MUTATION_MODE})" : ''))
            }
          }
        } catch (Throwable e) {
          echo "notifyTelegram: no node: ${e}"
        }
      }
    }
  }
}
