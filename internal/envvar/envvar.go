// Package envvar is the one table of the product's environment variables.
//
// Every product variable has a VH_ name and an older BOARD_ alias, and the
// code reads both through Var.Get, never through os.Getenv or os.LookupEnv
// with a literal name (the static test in this package fails on that).
//
// The rule, for each variable: VH_<X> is used when it is set and not empty;
// otherwise BOARD_<X> is used when it is set and not empty; otherwise the
// variable is unset, and an empty value counts as unset. When both are set
// VH_ wins silently, and using the old name never prints a warning. A message
// that names a variable uses the VH_ name.
//
// The variables that are new in the Village Hall phase (VH_VERSION,
// VH_INSTALL_DIR, VH_REQUIRE_IMMUTABLE, VH_REPO and the test base-URL
// overrides) have no BOARD_ alias and are not in this table. The CI-internal
// BOARD_CI_*, BOARD_GODOG_*, BOARD_DB_LEG, BOARD_MUTATION_*, BOARD_IMAGE_*,
// BOARD_ALLURE_* and BOARD_FEATURES_* names are not product variables either.
package envvar

import "strings"

const (
	vhPrefix    = "VH_"
	boardPrefix = "BOARD_"
)

// Var is one product variable, known by its suffix after VH_ or BOARD_.
type Var struct{ suffix string }

var all []Var

func define(suffix string) Var {
	v := Var{suffix}
	all = append(all, v)
	return v
}

// The 25 product variables.
var (
	URL                  = define("URL")
	APIKey               = define("API_KEY")
	Actor                = define("ACTOR")
	WayBack              = define("WAY_BACK")
	PublicURL            = define("PUBLIC_URL")
	Listen               = define("LISTEN")
	DBURL                = define("DB_URL")
	Migrate              = define("MIGRATE")
	SQLiteCheckpoint     = define("SQLITE_CHECKPOINT")
	SweepInterval        = define("SWEEP_INTERVAL")
	NotifyURL            = define("NOTIFY_URL")
	NotifyOn             = define("NOTIFY_ON")
	NotifyHeader         = define("NOTIFY_HEADER")
	NotifyTimeout        = define("NOTIFY_TIMEOUT")
	OIDCIssuer           = define("OIDC_ISSUER")
	OIDCClientID         = define("OIDC_CLIENT_ID")
	OIDCClientSecret     = define("OIDC_CLIENT_SECRET")
	OIDCClientSecretFile = define("OIDC_CLIENT_SECRET_FILE")
	OIDCAllowedEmails    = define("OIDC_ALLOWED_EMAILS")
	OIDCOperators        = define("OIDC_OPERATORS")
	OperatorHandle       = define("OPERATOR_HANDLE")
	SessionTTL           = define("SESSION_TTL")
	SessionIdle          = define("SESSION_IDLE")
	StreamPoll           = define("STREAM_POLL")
	StreamHeartbeat      = define("STREAM_HEARTBEAT")
)

// All lists the table's variables in declaration order.
func All() []Var { return append([]Var(nil), all...) }

// Name is the VH_ name, the one every message uses.
func (v Var) Name() string { return vhPrefix + v.suffix }

// Alias is the older BOARD_ name, still accepted.
func (v Var) Alias() string { return boardPrefix + v.suffix }

// Suffix is the name after VH_ or BOARD_.
func (v Var) Suffix() string { return v.suffix }

// Get reads the variable through getenv: VH_ when set and not empty, then
// BOARD_ when set and not empty, then "" (unset). It never warns.
func (v Var) Get(getenv func(string) string) string {
	if got := getenv(v.Name()); got != "" {
		return got
	}
	return getenv(v.Alias())
}

// Unset is the message for a variable that is needed and not set.
func (v Var) Unset() string {
	return v.Name() + " is unset (" + v.Alias() + " is also accepted)"
}

// Known reports whether name is a VH_ or BOARD_ name of a table variable.
func Known(name string) bool {
	for _, p := range []string{vhPrefix, boardPrefix} {
		if s, ok := strings.CutPrefix(name, p); ok {
			for _, v := range all {
				if v.suffix == s {
					return true
				}
			}
		}
	}
	return false
}
