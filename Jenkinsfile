// board CI on the homelab Jenkins: multibranch job board/ci (see docs/ci.md).
//
// Every branch: CI image -> Lint -> Suite -> Quality; main also runs nightly
// (03:17) in the extended lane (-race, fresh vulnerability databases, three
// clean-state stability runs), which the EXTENDED parameter asks for on
// demand. Mutation (ci/mutation.sh, the Go mutation gate) runs last, on every
// main build and in the extended lane; other branches skip it. A fully green
// main build archives ci-results/release-candidate.json. Reports are Jenkins
// HTML reports on the build page ("Allure", "CI summary"), built in
// post { always } so a red build still has them.
//
// Everything Go runs in the board-ci image through ci/run.sh, like a local
// run; only Allure (needs the agent's JRE) and ci/stability.sh (starts its
// own ci/run.sh siblings) run on the agent itself.
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
    copyArtifactPermission('/board/ci/*')
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
            sh 'ci/run.sh ci/quality.sh "$BOARD_CI_OUT"'
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
      archiveArtifacts artifacts: 'ci-results/**/*.txt, ci-results/**/*.xml, ci-results/**/*.out, ci-results/mutation/*.json, ci-results/stability/**/*.log, ci-summary/**, allure-history/**',
                       allowEmptyArchive: true
    }
    // Only a fully green main build (SUCCESS, not UNSTABLE) is a release
    // candidate; success {} runs after always {}.
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
    // build makes a sound.
    cleanup {
      script {
        def watched = env.BRANCH_NAME == 'main' || env.CI_NIGHTLY == 'true'
        notifyTelegram(notifyOn: 'ALL', silent: watched ? null : true,
                       extra: env.CI_NIGHTLY == 'true' ? 'nightly (extended lane)' : null)
      }
    }
  }
}
