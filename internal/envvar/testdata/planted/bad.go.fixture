package planted

import "os"

var _ = os.Getenv

func reads() {
	_ = os.Getenv("BOARD_URL")
	_ = os.Getenv("VH_API_KEY")
	_, _ = os.LookupEnv("BOARD_ACTOR")
	_, _ = os.LookupEnv("VH_LISTEN")
	_ = os.Getenv("HOME")
	_ = os.Getenv("BOARD_URL"[:0])
	name := "BOARD_URL"
	_ = os.Getenv(name)
}

func other(env func(string) string) string { return env("BOARD_URL") }
