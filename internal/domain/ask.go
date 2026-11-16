package domain

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Limits of an ask (SPEC 4.5).
const (
	MaxQuestion     = 200
	MaxAnswer       = 20000
	MaxOptions      = 10
	MaxOptionLen    = 80
	MaxWithdrawNote = 200
)

// Ask states.
const (
	AskOpen      = "open"
	AskAnswered  = "answered"
	AskWithdrawn = "withdrawn"
)

// Ask modes: stop ends the asker's turn, poll keeps a long poll open.
const (
	AskModeStop = "stop"
	AskModePoll = "poll"
)

// Ways an answer reached the board.
const (
	ChannelUI    = "ui"
	ChannelAPI   = "api"
	ChannelRelay = "relay"
)

// FormatAskRef is the short ref A<n> of the ask numbered n.
func FormatAskRef(n int64) string { return "A" + strconv.FormatInt(n, 10) }

// ParseAskRef reads A<n> (A in either case, n a positive integer without a sign).
func ParseAskRef(s string) (int64, bool) {
	if len(s) < 2 || (s[0] != 'A' && s[0] != 'a') {
		return 0, false
	}
	n, err := parseSeq(s[1:])
	return int64(n), err == nil
}

// ValidateQuestion checks an ask's question: 1 to 200 characters on one line.
func ValidateQuestion(q string) error {
	q = strings.TrimSpace(q)
	if n := utf8.RuneCountInString(q); n < 1 || n > MaxQuestion {
		return fmt.Errorf("question must be 1 to %d characters, got %d", MaxQuestion, n)
	}
	if strings.ContainsAny(q, "\r\n") {
		return fmt.Errorf("question must be one line")
	}
	return nil
}

// ValidateAnswer checks an answer: 1 to 20000 characters once trimmed.
func ValidateAnswer(a string) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(a)); n < 1 || n > MaxAnswer {
		return fmt.Errorf("answer must be 1 to %d characters, got %d", MaxAnswer, n)
	}
	return nil
}

// ValidateOptions checks an ask's suggested answers: at most 10, each 1 to 80
// characters.
func ValidateOptions(options []string) error {
	if len(options) > MaxOptions {
		return fmt.Errorf("options holds at most %d entries, got %d", MaxOptions, len(options))
	}
	for _, o := range options {
		if n := utf8.RuneCountInString(o); n < 1 || n > MaxOptionLen {
			return fmt.Errorf("an option must be 1 to %d characters, got %d", MaxOptionLen, n)
		}
	}
	return nil
}

// ValidateAskMode returns nil for stop or poll.
func ValidateAskMode(mode string) error {
	if mode != AskModeStop && mode != AskModePoll {
		return fmt.Errorf("mode %q must be stop or poll", mode)
	}
	return nil
}

// ValidateChannel returns nil for ui, api or relay.
func ValidateChannel(channel string) error {
	switch channel {
	case ChannelUI, ChannelAPI, ChannelRelay:
		return nil
	}
	return fmt.Errorf("channel %q must be ui, api or relay", channel)
}

// What raising an ask does to the asker's live lease on the task: none leaves
// it, keep leaves it live on purpose and stop ends it as blocked on the ask.
const (
	LeaseActionNone = "none"
	LeaseActionKeep = "keep"
	LeaseActionStop = "stop"
)

// ValidateLeaseAction returns nil for none, keep or stop.
func ValidateLeaseAction(action string) error {
	switch action {
	case LeaseActionNone, LeaseActionKeep, LeaseActionStop:
		return nil
	}
	return fmt.Errorf("lease_action %q must be none, keep or stop", action)
}
