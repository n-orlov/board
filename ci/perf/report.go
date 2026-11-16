package perf

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Report is what one run writes to perf.json.
type Report struct {
	StartedAt  time.Time          `json:"started_at"`
	FinishedAt time.Time          `json:"finished_at"`
	Config     Config             `json:"config"`
	Reduced    bool               `json:"reduced"`
	Metrics    map[string]float64 `json:"metrics"`
	Checks     []ReportCheck      `json:"checks"`
	Passed     bool               `json:"passed"`
}

// ReportCheck is one gate's line of the report.
type ReportCheck struct {
	Target   string  `json:"target"`
	Name     string  `json:"name"`
	Metric   string  `json:"metric"`
	Unit     string  `json:"unit"`
	Limit    float64 `json:"limit"`
	Measured bool    `json:"measured"`
	Value    float64 `json:"value"`
	Pass     bool    `json:"pass"`
	Detail   string  `json:"detail"`
}

// NewReport judges the metrics of a run and states the verdict: passed only when
// every gate passed. A reduced run states that it was one.
func NewReport(cfg Config, metrics map[string]float64, started, finished time.Time) Report {
	checks := Evaluate(metrics)
	r := Report{StartedAt: started, FinishedAt: finished, Config: cfg, Reduced: cfg.Reduced(), Metrics: metrics,
		Passed: len(Failures(checks)) == 0}
	for _, c := range checks {
		r.Checks = append(r.Checks, ReportCheck{Target: c.Target, Name: c.Name, Metric: c.Metric, Unit: c.Unit, Limit: c.Max,
			Measured: c.Measured, Value: c.Value, Pass: c.Pass, Detail: c.Detail})
	}
	return r
}

// WriteJSON writes the report to path, making its directory.
func (r Report) WriteJSON(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(data, '\n'))
}

type junitSuites struct {
	XMLName xml.Name    `xml:"testsuites"`
	Suites  []junitSuit `xml:"testsuite"`
}

type junitSuit struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Time     string      `xml:"time,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Classname string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

// WriteJUnit writes one test case per gate to path: a gate that is breached or
// was not measured is a failure. The suite name says when the run was reduced.
func (r Report) WriteJUnit(path string) error {
	name := "perf"
	if r.Reduced {
		name = "perf (reduced smoke run)"
	}
	suite := junitSuit{Name: name, Time: seconds(r.FinishedAt.Sub(r.StartedAt))}
	for _, c := range r.Checks {
		tc := junitCase{Classname: "perf." + c.Target, Name: c.Name, Time: "0"}
		if !c.Pass {
			tc.Failure = &junitFailure{Message: c.Detail, Text: fmt.Sprintf("%s: %s", c.Metric, c.Detail)}
			suite.Failures++
		}
		suite.Cases = append(suite.Cases, tc)
	}
	suite.Tests = len(suite.Cases)
	data, err := xml.MarshalIndent(junitSuites{Suites: []junitSuit{suite}}, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append([]byte(xml.Header), append(data, '\n')...))
}

func seconds(d time.Duration) string { return fmt.Sprintf("%.3f", d.Seconds()) }

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
