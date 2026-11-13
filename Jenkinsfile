// board CI on the homelab Jenkins: multibranch job board/ci (see docs/ci.md).
//
// EXPERIMENT (branch ci/distributed, cut from 169bb59): every test stage is a
// "unit" that runs in parallel on its own Jenkins node from the board-test
// pool (the homelab phones plus int21h-ws), instead of one stage after
// another on int21h-ws. Units ask for capabilities by label, most preferred
// first (Suite and Quality prefer amd64; Image needs it, because
// ci/image-check.sh runs the amd64 image), and never name a node.
//
// Retry policy, per unit:
//   - infrastructure failure (agent lost, Docker unavailable, checkout or CI
//     image build failed, the attempt timed out or went silent, no node free):
//     the unit moves to a node it has not tried, up to 3 infrastructure
//     attempts;
//   - the unit's command fails: one rerun on a different node; a pass there
//     marks the build UNSTABLE ("flaky on <node>"), a second failure fails it.
// Each attempt reports node, queue wait and run time; the build page gets a
// per-unit timing table (ci-results/distributed/timing.md).
//
// Each unit checks out the build's commit on its node, builds board-ci:local
// there (ci/jenkins-image.sh; cached per Docker daemon), runs, removes the
// sibling containers it labelled (ralphd.run=<attempt id>, by exact ID) and
// stashes its ci-results. Quality waits for Suite (coverage) and Image (the
// image tar). The report runs once at the end, on any pool node, over every
// unit's stash.
//
// Three phases, one after another, so the pool never runs all three test
// kinds at once; each phase's units run in parallel:
//   1. Core (every build): Lint, Suite, Image, Allure smoke, Quality.
//   2. Stability (EXTENDED=true, or nightly on main): three stability runs,
//      one unit each; EXTENDED also adds -race and fresh vulnerability
//      databases to Core.
//   3. Mutation (MUTATION=true, or nightly on main): the seven mutation lanes.
// A phase runs only if every earlier one passed (UNSTABLE counts as passed).
@Library('homelab') _
import groovy.transform.Field

// Units' results and attempts, shared by the parallel branches.
@Field def results = [:]        // unit -> 'pass' | 'flaky' | 'fail'
@Field def attemptLog = [:]     // unit -> [attempt maps]
@Field def signals = [:]        // 'Suite' / 'Image' -> true once stashed (pass or fail)
@Field String commitSha = ''
@Field String imageVersion = ''
@Field final String AMD = 'board-test && amd64 && docker'
@Field final String ANY = 'board-test && docker'

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

// Runs one unit. u: name, labels (most preferred first; retries widen to the
// last), minutes (budget on amd64; arm64 gets u.armFactor x, default 3),
// body (closure on the node, returns the command's exit status), needs
// (stash names to unstash first), extraStash (closure on the node, after a run),
// silentMinutes (how long the command may print nothing, default 45: gremlins
// and the suite's later passes print only when they finish).
def runUnit(Map u) {
  def tried = []
  def attempts = []
  attemptLog[u.name] = attempts
  int infra = 0
  boolean rerun = false
  String firstFail = null
  int armFactor = u.armFactor ?: 3
  while (true) {
    String want = attempts.isEmpty() ? u.labels[0] : u.labels[-1]
    String expr = '(' + want + ')' + tried.collect { ' && !' + it }.join('')
    def a = [n: attempts.size() + 1, label: expr, node: null, queued: nowMs(), start: 0L, end: 0L, outcome: null]
    attempts << a
    String runId = "jenkins-${env.BUILD_NUMBER}-${slug(u.name)}-${a.n}"
    try {
      // Queue wait (no free node matching) is capped too: 45 minutes.
      timeout(time: 45 + u.minutes * armFactor, unit: 'MINUTES') {
        node(expr) {
          a.node = env.NODE_NAME
          a.start = nowMs()
          tried << env.NODE_NAME
          boolean arm = !(env.NODE_LABELS ?: '').split(' ').contains('amd64')
          a.arch = arm ? 'arm64' : 'amd64'
          echo "unit ${u.name}: attempt ${a.n} on ${env.NODE_NAME} (${a.arch}), label ${expr}"
          try {
            // A silent attempt (agent gone, hung container) ends after
            // silentMinutes.
            timeout(time: u.minutes * (arm ? armFactor : 1), unit: 'MINUTES') {
              timeout(time: u.silentMinutes ?: 45, unit: 'MINUTES', activity: true) {
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
      if (!rerun && attempts.size() < 4) {
        rerun = true
        firstFail = a.node
        echo "unit ${u.name}: failed on ${a.node}; one rerun on another node"
        continue
      }
      results[u.name] = 'fail'
      error("${u.name}: failed on ${firstFail ?: a.node}" + (firstFail ? " and ${a.node}" : ''))
    }
    infra++
    if (infra >= 3 || attempts.size() >= 4) {
      results[u.name] = 'fail'
      error("${u.name}: ${infra} infrastructure failures (${attempts.collect { (it.node ?: 'none') + ' ' + it.outcome }.join(', ')})")
    }
  }
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
  def lines = ['| unit | result | attempt | node | arch | queue (min) | run (min) | outcome |',
               '|------|--------|---------|------|------|-------------|-----------|---------|']
  attemptLog.keySet().sort().each { name ->
    attemptLog[name].each { a ->
      def q = a.start ? String.format('%.1f', (a.start - a.queued) / 60000.0) : String.format('%.1f', ((a.end ?: nowMs()) - a.queued) / 60000.0)
      def r = a.start ? String.format('%.1f', ((a.end ?: nowMs()) - a.start) / 60000.0) : '-'
      lines << "| ${name} | ${results[name] ?: '?'} | ${a.n} | ${a.node ?: '-'} | ${a.arch ?: '-'} | ${q} | ${r} | ${a.outcome} |"
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
    timeout(time: 300, unit: 'MINUTES')
    copyArtifactPermission('/board/release,/board/ci/*')
  }

  parameters {
    booleanParam(name: 'EXTENDED', defaultValue: false,
                 description: 'Extended CI: -race, fresh vulnerability databases, then the stability runs')
    booleanParam(name: 'MUTATION', defaultValue: false,
                 description: 'Mutation testing: the seven mutation lanes, after Core (and Stability if extended)')
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
          env.CI_MUTATION = (params.MUTATION || timer) ? 'true' : 'false'
          env.BOARD_CI_GO_EXTRA_FLAGS = nightly ? '-race' : ''
          def lanes = ['core'] + (nightly ? ['stability'] : []) + (env.CI_MUTATION == 'true' ? ['mutation'] : [])
          currentBuild.description = 'distributed: ' + lanes.join(' + ') + (nightly ? ' (-race)' : '')
          def units = []
          // gofmt, go vet, tidy drift, golangci-lint.
          units << [name: 'Lint', labels: [ANY], minutes: 30, body: { sh(returnStatus: true, script: 'ci/run.sh ci/lint.sh') }]
          units << [name: 'Suite', labels: [AMD, ANY], minutes: nightly ? 120 : 45, armFactor: 2, silentMinutes: 60, signal: 'Suite',
                    body: { sh(returnStatus: true, script: 'ci/run.sh ci/suite.sh') }]
          // Both platforms cross-built and never pushed; the amd64 image is run
          // and probed, so amd64 only.
          units << [name: 'Image', labels: [AMD], minutes: 30, signal: 'Image',
                    body: {
                      sh(returnStatus: true, script: 'ci/image-build.sh "$BOARD_CI_OUT/board-image.tar" && ci/image-check.sh "$BOARD_CI_OUT"')
                    },
                    extraStash: { stash name: 'image', includes: 'ci-results/board-image.tar,ci-results/board-image-oci.tar', allowEmpty: true }]
          units << [name: 'Allure smoke', labels: [ANY], minutes: 15,
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
                runUnit([name: 'Quality', labels: [AMD, ANY], minutes: 30, needs: ['res-suite', 'res-image', 'image'],
                         body: {
                           if (env.CI_NIGHTLY == 'true') {
                             sh "ci/run.sh sh -c 'rm -rf /go-cache/trivy/db /go-cache/trivy/java-db /go-cache/xdg-cache/govulncheck'"
                           }
                           // crap-ts and the openapi gates read web/node_modules, which
                           // Suite installed on its own node, not this one.
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
            // stability.sh prints only at the end (14 min on kilo, 22+ on golf).
            def u = [name: "Stability ${i}", labels: [ANY], minutes: 40, silentMinutes: 60,
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
          def branches = [:]
          // The Go mutation lanes (ci/quality.json mutation_go.lanes); base
          // also runs Stryker over web/src/lib. server-core gets 50 min: 35
          // ran out on a busy int21h-ws (#12). gremlins prints nothing until a
          // lane ends (server-streams went 50 min silent on bravo in #12), so
          // the silence limit covers the whole arm64 budget.
          ['logic', 'server-ops', 'server-streams', 'server-core', 'cli', 'cli-core', 'base'].each { lane ->
            int minutes = lane == 'server-core' ? 50 : 35
            def u = [name: "Mutation ${lane}", labels: [ANY], minutes: minutes, silentMinutes: minutes * 3,
                     body: { sh(returnStatus: true, script: "ci/run.sh ci/mutation.sh ci-results/mutation ci/quality.json ${lane}") }]
            branches[u.name] = unitBranch(u)
          }
          parallel branches
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
            node('board-test && docker && amd64 || board-test && docker') {
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
              archiveArtifacts artifacts: 'ci-results/**/*.txt, ci-results/**/*.xml, ci-results/**/*.out, ci-results/*.cdx.json, ci-results/mutation/*.json, ci-results/stability/**/*.log, ci-results/distributed/**, ci-summary/**, allure-history/**',
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
            node('board-test') {
              notifyTelegram(notifyOn: 'ALL', silent: watched ? null : true,
                             extra: 'distributed' + (env.CI_NIGHTLY == 'true' ? ', extended lane' : '') +
                                    (env.CI_MUTATION == 'true' ? ', mutation' : ''))
            }
          }
        } catch (Throwable e) {
          echo "notifyTelegram: no node: ${e}"
        }
      }
    }
  }
}
