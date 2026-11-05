// board CI on the homelab Jenkins: multibranch job board/ci (see docs/ci.md).
//
// Every branch: CI image -> Lint -> Suite -> Allure smoke -> Quality; main
// also runs nightly (03:17) in the extended lane (-race, fresh vulnerability
// databases, three clean-state stability runs), which the EXTENDED parameter
// asks for on demand. Mutation (ci/mutation.sh, the Go and frontend mutation
// gates) runs last, on every main build and in the extended lane; other
// branches skip it. A fully green main build archives
// ci-results/release-candidate.json, which Jenkinsfile.release (the
// board/release job, a placeholder in this phase) validates against; this job
// never releases. Reports are Jenkins HTML reports on the build page
// ("Allure", "CI summary"), built in post { always } so a red build still has
// them.
//
// Everything Go runs in the board-ci image through ci/run.sh, like a local
// run; only Allure (needs the agent's JRE), the container image build (needs
// the docker CLI) and ci/stability.sh (starts its own ci/run.sh siblings) run
// on the agent itself.
//
// Telegram: notifyTelegram from the homelab shared library
// (github.com/n-orlov/jenkins-library), last, in post { cleanup }.
@Library('homelab') _

pipeline {
  agent { label 'linux && amd64 && docker' }

  options {
    ansiColor('xterm')
    timestamps()
    buildDiscarder(logRotator(numToKeepStr: '30'))
    disableConcurrentBuilds()
    timeout(time: 200, unit: 'MINUTES')   // ceiling; stages carry their own
    // board/release reads release-candidate.json; this job's own next build
    // reads allure-history/ (patterns match the copier's absolute name).
    copyArtifactPermission('/board/release,/board/ci/*')
  }

  parameters {
    booleanParam(name: 'EXTENDED', defaultValue: false,
                 description: 'Extended CI: -race, fresh vulnerability databases, stability runs, mutation')
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
    stage('CI image') {
      steps {
        script {
          def nightly = params.EXTENDED || !currentBuild.getBuildCauses('hudson.triggers.TimerTrigger$TimerTriggerCause').isEmpty()
          env.CI_NIGHTLY = nightly ? 'true' : 'false'
          env.BOARD_CI_GO_EXTRA_FLAGS = nightly ? '-race' : ''
          env.BOARD_CI_SHA = env.GIT_COMMIT ?: ''
          if (nightly) {
            currentBuild.description = 'extended (-race, stability, mutation)'
          }
        }
        // The workspace persists between builds: start from the checkout alone.
        sh 'git clean -ffdxq'
        sh 'ci/jenkins-image.sh'
      }
    }

    // gofmt -l, go vet, go mod tidy drift, golangci-lint: fails fast, before the suite.
    stage('Lint') {
      options { timeout(time: 30, unit: 'MINUTES') }
      steps {
        sh 'ci/run.sh ci/lint.sh'
      }
    }

    stage('Suite') {
      steps {
        timeout(time: env.CI_NIGHTLY == 'true' ? 180 : 45, unit: 'MINUTES') {
          catchError(buildResult: 'FAILURE', stageResult: 'FAILURE') {
            sh 'ci/run.sh ci/suite.sh'
          }
        }
      }
    }

    // The pinned Allure CLI still renders the Godog formatter's output.
    stage('Allure smoke') {
      options { timeout(time: 15, unit: 'MINUTES') }
      steps {
        catchError(buildResult: 'FAILURE', stageResult: 'FAILURE') {
          sh '''
            mkdir -p allure-smoke/fixture/allure-results
            BOARD_ALLURE_FIXTURE_OUT=/w/allure-smoke/fixture/allure-results \
              ci/run.sh go test -count=1 -run '^TestAllureFormatterWritesResultsForAFixtureRun$' ./features/
            ALLURE_CACHE_DIR="$HOME/.cache/board-allure" ci/allure-smoke.sh allure-smoke/fixture allure-smoke/report
          '''
        }
      }
    }

    // The mutation gates (mutation-go, stryker) read the Mutation stage's
    // reports, which are written after this stage and not at all on a branch
    // build, so --mutation-deferred reports them as judged there: ci/mutation.sh
    // reads the same thresholds from ci/quality.json and fails the build below
    // them. Every other gate runs here. The image the Trivy and SBOM image
    // gates scan is built on the agent (it needs the docker CLI) and never pushed.
    stage('Quality') {
      options { timeout(time: 30, unit: 'MINUTES') }
      steps {
        script {
          if (env.CI_NIGHTLY == 'true') {
            // Nightly vulnerability rescan: drop the cached databases.
            sh "ci/run.sh sh -c 'rm -rf /go-cache/trivy/db /go-cache/trivy/java-db /go-cache/xdg-cache/govulncheck'"
          }
        }
        catchError(buildResult: 'FAILURE', stageResult: 'FAILURE') {
          script {
            env.QUALITY_RESULT = 'failing'
            sh 'ci/image-build.sh "$BOARD_CI_OUT/board-image.tar"'
            sh 'ci/run.sh ci/quality.sh "$BOARD_CI_OUT" ci/quality.json --mutation-deferred'
            env.QUALITY_RESULT = 'passing'
          }
        }
      }
    }

    // Three clean-state runs. On the agent, not in ci/run.sh:
    // ci/stability.sh starts one ci/run.sh sibling per run itself.
    stage('Stability') {
      when { environment name: 'CI_NIGHTLY', value: 'true' }
      steps {
        timeout(time: 120, unit: 'MINUTES') {
          catchError(buildResult: 'FAILURE', stageResult: 'FAILURE') {
            sh '''
              mkdir -p "$BOARD_CI_OUT/stability"
              TMPDIR="$PWD/$BOARD_CI_OUT/stability" ci/stability.sh 3
            '''
          }
        }
      }
    }

    // The Go mutation gate (SPEC 15.4): gremlins over internal/domain and
    // internal/store, efficacy >= 80 % per package from ci/quality.json, a
    // package with no mutant fails. Every main build and the extended lane.
    stage('Mutation') {
      when {
        anyOf {
          branch 'main'
          environment name: 'CI_NIGHTLY', value: 'true'
        }
      }
      options { timeout(time: 30, unit: 'MINUTES') }
      steps {
        catchError(buildResult: 'FAILURE', stageResult: 'FAILURE') {
          sh 'ci/run.sh ci/mutation.sh'
        }
      }
    }
  }

  post {
    always {
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
      script {
        // README badges (public via https://int21h.mooo.com/jenkins-static/
        // board/ci/<branch>/<id>.svg; "build" is the plugin's own). Declared on
        // every build, so a build without counts shows "not run", not the
        // previous build's numbers.
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
      }
      // ci-results/stability/**/*.log: ci/stability.sh's per-run go test logs,
      // the only full record of a stability failure (the console has an excerpt).
      archiveArtifacts artifacts: 'ci-results/**/*.txt, ci-results/**/*.xml, ci-results/**/*.out, ci-results/*.cdx.json, ci-results/mutation/*.json, ci-results/stability/**/*.log, ci-summary/**, allure-history/**',
                       allowEmptyArchive: true
    }
    // Only a fully green main build (SUCCESS, not UNSTABLE) is a release
    // candidate; success {} runs after always {}, so the reports cannot turn
    // it unstable afterwards. board/release copies this file.
    success {
      script {
        if (env.BRANCH_NAME == 'main') {
          writeFile file: 'ci-results/release-candidate.json',
                    text: '{"commit": "' + env.GIT_COMMIT + '", "build_url": "' + env.BUILD_URL +
                          '", "build_number": ' + env.BUILD_NUMBER + '}\n'
          archiveArtifacts artifacts: 'ci-results/release-candidate.json', fingerprint: true
        }
      }
    }
    // One Telegram message per build; only a red or aborted main or nightly
    // build makes a sound; everything else arrives silently. cleanup runs after
    // success {}, so the message carries the final result.
    cleanup {
      script {
        def watched = env.BRANCH_NAME == 'main' || env.CI_NIGHTLY == 'true'
        notifyTelegram(notifyOn: 'ALL', silent: watched ? null : true,
                       extra: env.CI_NIGHTLY == 'true' ? 'nightly (extended lane)' : null)
      }
    }
  }
}
