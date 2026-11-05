# CI

This is the only CI document. It grows with the pipeline; this section covers
the toolchain image and how every command runs in it.

## Toolchain image

`ci/Dockerfile` builds `board-ci:local`:

- Go on the 1.26.x line, Node 24, and the Playwright 1.64.1 browsers
  (chromium, firefox, webkit) under `/ms-playwright`;
- gotestsum, golangci-lint, govulncheck, gremlins, shellcheck and trivy.

Every `FROM` and every `COPY --from` image is pinned by `@sha256:` digest, and
the tool versions are pinned in the file. `ci/repocheck/dockerfiledigest_test.go`
fails on an unpinned one. Build it once by hand:

    docker build -t board-ci:local -f ci/Dockerfile ci

## Running a command: `ci/run.sh`

`ci/run.sh <command...>` runs the command in a throwaway sibling container of
the image above, on the host docker daemon:

- the bind source is `$RALPHD_HOST_WORKSPACE` (a HOST path), else `$PWD`;
- the sibling runs as the caller's `uid:gid`, so it leaves no root-owned files;
- two named volumes carry the caches: `board-go-cache` (`/go-cache`: Go module,
  build and golangci-lint caches) and `board-npm-cache` (`/npm-cache`);
- every `BOARD_*` variable in the caller's environment is forwarded, and no
  other variable is. `BOARD_CI_IMAGE`, `BOARD_CI_CACHE_VOLUME` and
  `BOARD_CI_NPM_VOLUME` override the image and the volume names.

`ci/repocheck/runsh_test.go` runs the script against a stub `docker` and checks
that argv.

## Disposable-clone measurement: `ci/review.sh`

`ci/review.sh` clones the repository into the git-ignored `.review-clone/` and,
through `ci/run.sh`, asserts that the module path is `github.com/n-orlov/board`
and that `go list ./internal/...` resolves only to directories under the clone,
never the original checkout. With no argument (or `--smoke`) it stops there and
exits 0; any other arguments are passed to `go test` inside the same clone.
The clone is of the committed HEAD, so commit before running it.
