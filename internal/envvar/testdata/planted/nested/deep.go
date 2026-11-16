package nested

import "os"

func read() string {
	return os.Getenv("BOARD_DB_URL")
}
