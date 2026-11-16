Feature: Updating the CLI, vh update
  vh update [--version vX.Y.Z] [--check] replaces the running binary with the latest release, or the pinned
  one, after checking that the release is immutable and that the archive matches checksums.txt. --check only
  says whether a newer release exists, and writes nothing. Every refusal exits 1 with one line that has a
  arrow, and leaves the binary as it was. The release comes from VH_REPO at VH_API_URL and VH_DOWNLOAD_URL; no
  scenario reaches the real GitHub.

  Scenario: --check says an update is available and writes nothing
    Given the vh binary stamped as version "v1.0.0" is installed in its own directory
    And a release server publishes "v1.1.0" of example/board as immutable
    When I run the installed vh 'update --check' against the release server
    Then the exit code is 0
    And stdout is exactly 'update available: v1.0.0 → v1.1.0'
    And stderr is empty
    And the installed vh is unchanged

  Scenario: --check says the binary is up to date when the latest release is the one running
    Given the vh binary stamped as version "v1.1.0" is installed in its own directory
    And a release server publishes "v1.1.0" of example/board as immutable
    When I run the installed vh 'update --check' against the release server
    Then the exit code is 0
    And stdout is exactly 'up to date (v1.1.0)'
    And the installed vh is unchanged

  Scenario: an update replaces the running binary at the same path with mode 0755
    Given the vh binary stamped as version "v1.0.0" is installed in its own directory
    And a release server publishes "v1.1.0" of example/board as immutable
    When I run the installed vh 'update' against the release server
    Then the exit code is 0
    And stdout line 1 matches '^updated: v1\.0\.0 → v1\.1\.0 \(.*/vh\)$'
    And stderr is empty
    And the installed vh holds the published binary
    And the installed vh has mode 0755

  Scenario: a release that is not immutable is refused and the binary stays
    Given the vh binary stamped as version "v1.0.0" is installed in its own directory
    And a release server publishes "v1.1.0" of example/board as not immutable
    When I run the installed vh 'update' against the release server
    Then the exit code is 1
    And stdout is empty
    And stderr is one line holding "not an immutable release" and an arrow
    And the installed vh is unchanged

  Scenario: a version that does not exist is refused and the binary stays
    Given the vh binary stamped as version "v1.0.0" is installed in its own directory
    And a release server publishes "v1.1.0" of example/board as immutable
    When I run the installed vh 'update --version v9.9.9' against the release server
    Then the exit code is 1
    And stderr is one line holding "v9.9.9" and an arrow
    And the installed vh is unchanged

  Scenario: a checkout build is refused as a dev build
    Given a release server publishes "v1.1.0" of example/board as immutable
    When I run the checkout build of vh 'update --check' against the release server
    Then the exit code is 1
    And stdout is empty
    And stderr is one line holding "dev build" and an arrow
