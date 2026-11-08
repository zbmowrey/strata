package gates

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/finding"
)

// Parser names (docs/spec/06-gates.md §6).
const (
	ParserGolangci   = "golangci-json"
	ParserRuff       = "ruff-json"
	ParserPyright    = "pyright-json"
	ParserESLint     = "eslint-json"
	ParserTSC        = "tsc-text"
	ParserSemgrep    = "semgrep-sarif"
	ParserGitleaks   = "gitleaks-json"
	ParserShellcheck = "shellcheck-json"
	ParserClippy     = "clippy-json"
	ParserGoTest     = "go-test-json"
	ParserSARIF      = "sarif"
	ParserLines      = "lines"
	ParserExitcode   = "exitcode"
)

// Output is what a tool produced.
type Output struct {
	Stdout []byte
	Stderr []byte
	Exit   int
}

// parseFunc normalizes tool output into findings. File paths are returned as
// the tool printed them; the runner makes them repo-relative.
type parseFunc func(o Output, g *Gate) ([]finding.Finding, error)

var parsers = map[string]parseFunc{
	ParserGolangci:   parseGolangci,
	ParserRuff:       parseRuff,
	ParserPyright:    parsePyright,
	ParserESLint:     parseESLint,
	ParserTSC:        parseTSC,
	ParserSemgrep:    parseSARIF,
	ParserGitleaks:   parseGitleaks,
	ParserShellcheck: parseShellcheck,
	ParserClippy:     parseClippy,
	ParserGoTest:     parseGoTest,
	ParserSARIF:      parseSARIF,
	ParserLines:      parseLines,
	ParserExitcode:   parseExitcode,
}

// ParseOutput runs the named parser.
func ParseOutput(name string, o Output, g *Gate) ([]finding.Finding, error) {
	p, ok := parsers[name]
	if !ok {
		return nil, fmt.Errorf("unknown parser %q", name)
	}
	return p(o, g)
}

var errNoJSON = errors.New("no JSON in tool output")

// decodeFirst decodes the first JSON value in b (tools often append text).
func decodeFirst(b []byte, v any) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return errNoJSON
	}
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(v); err != nil {
		return fmt.Errorf("decode tool output: %w", err)
	}
	return nil
}

// severity normalizes tool severities.
func severity(s, def string) string {
	switch strings.ToLower(s) {
	case "error", "fatal", "critical", "high", "2":
		return finding.Error
	case "warning", "warn", "medium", "1":
		return finding.Warning
	case "info", "information", "note", "style", "low", "hint", "0":
		return finding.Info
	case "":
		if def != "" {
			return def
		}
		return finding.Error
	default:
		return finding.Warning
	}
}

func defSev(g *Gate) string {
	if g == nil {
		return ""
	}
	return g.Severity
}

func parseGolangci(o Output, g *Gate) ([]finding.Finding, error) {
	var r struct {
		Issues []struct {
			FromLinter string
			Text       string
			Severity   string
			Pos        struct {
				Filename     string
				Line, Column int
			}
		}
	}
	if err := decodeFirst(o.Stdout, &r); err != nil {
		return nil, err
	}
	out := make([]finding.Finding, 0, len(r.Issues))
	for _, is := range r.Issues {
		out = append(out, finding.Finding{
			Rule: is.FromLinter, Severity: severity(is.Severity, defSev(g)), File: is.Pos.Filename,
			Line: is.Pos.Line, Col: is.Pos.Column, Message: strings.TrimSpace(is.Text),
		})
	}
	return out, nil
}

func parseRuff(o Output, g *Gate) ([]finding.Finding, error) {
	var r []struct {
		Code     *string
		Message  string
		Filename string
		URL      *string
		Location struct{ Row, Column int }
		Fix      *struct{ Message *string }
	}
	if err := decodeFirst(o.Stdout, &r); err != nil {
		return nil, err
	}
	out := make([]finding.Finding, 0, len(r))
	for _, d := range r {
		f := finding.Finding{Rule: deref(d.Code, "syntax"), Severity: severity("", defSev(g)), File: d.Filename, Line: d.Location.Row, Col: d.Location.Column, Message: d.Message, Docs: deref(d.URL, "")}
		if d.Fix != nil && d.Fix.Message != nil {
			f.FixHint = *d.Fix.Message
		}
		out = append(out, f)
	}
	return out, nil
}

func deref(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

func parsePyright(o Output, g *Gate) ([]finding.Finding, error) {
	var r struct {
		GeneralDiagnostics []struct {
			File, Severity, Message, Rule string
			Range                         struct {
				Start struct{ Line, Character int }
			}
		}
	}
	if err := decodeFirst(o.Stdout, &r); err != nil {
		return nil, err
	}
	out := make([]finding.Finding, 0, len(r.GeneralDiagnostics))
	for _, d := range r.GeneralDiagnostics {
		rule := d.Rule
		if rule == "" {
			rule = "pyright"
		}
		out = append(out, finding.Finding{
			Rule: rule, Severity: severity(d.Severity, defSev(g)), File: d.File,
			Line: d.Range.Start.Line + 1, Col: d.Range.Start.Character + 1, Message: d.Message,
		})
	}
	return out, nil
}

func parseESLint(o Output, g *Gate) ([]finding.Finding, error) {
	var r []struct {
		FilePath string
		Messages []struct {
			RuleID   *string
			Severity int
			Message  string
			Line     int
			Column   int
		}
	}
	if err := decodeFirst(o.Stdout, &r); err != nil {
		return nil, err
	}
	var out []finding.Finding
	for _, file := range r {
		for _, m := range file.Messages {
			out = append(out, finding.Finding{
				Rule: deref(m.RuleID, "parse-error"), Severity: severity(strconv.Itoa(m.Severity), defSev(g)),
				File: file.FilePath, Line: m.Line, Col: m.Column, Message: m.Message,
			})
		}
	}
	return out, nil
}

var tscRE = regexp.MustCompile(`^(.+?)\((\d+),(\d+)\): (error|warning) (TS\d+): (.*)$`)

func parseTSC(o Output, g *Gate) ([]finding.Finding, error) {
	var out []finding.Finding
	sc := bufio.NewScanner(bytes.NewReader(append(append([]byte{}, o.Stdout...), o.Stderr...)))
	for sc.Scan() {
		m := tscRE.FindStringSubmatch(strings.TrimRight(sc.Text(), "\r"))
		if m == nil {
			continue
		}
		line, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		out = append(out, finding.Finding{Rule: m[5], Severity: severity(m[4], defSev(g)), File: m[1], Line: line, Col: col, Message: m[6]})
	}
	return out, nil
}

// parseSARIF handles SARIF 2.1 (semgrep and generic): the level comes from
// the result, else from the rule's defaultConfiguration.
func parseSARIF(o Output, g *Gate) ([]finding.Finding, error) {
	var r struct {
		Runs []struct {
			Tool struct {
				Driver struct {
					Rules []struct {
						ID                   string `json:"id"`
						HelpURI              string `json:"helpUri"`
						DefaultConfiguration struct {
							Level string `json:"level"`
						} `json:"defaultConfiguration"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID  string `json:"ruleId"`
				Level   string `json:"level"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine   int `json:"startLine"`
							StartColumn int `json:"startColumn"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := decodeFirst(o.Stdout, &r); err != nil {
		return nil, err
	}
	var out []finding.Finding
	for _, run := range r.Runs {
		levels, docs := map[string]string{}, map[string]string{}
		for _, rule := range run.Tool.Driver.Rules {
			levels[rule.ID], docs[rule.ID] = rule.DefaultConfiguration.Level, rule.HelpURI
		}
		for _, res := range run.Results {
			f := finding.Finding{Rule: res.RuleID, Message: strings.TrimSpace(res.Message.Text), Docs: docs[res.RuleID]}
			level := res.Level
			if level == "" {
				level = levels[res.RuleID]
			}
			f.Severity = severity(level, defSev(g))
			if len(res.Locations) > 0 {
				pl := res.Locations[0].PhysicalLocation
				f.File = strings.TrimPrefix(pl.ArtifactLocation.URI, "file://")
				f.Line, f.Col = pl.Region.StartLine, pl.Region.StartColumn
			}
			out = append(out, f)
		}
	}
	return out, nil
}

// parseGitleaks never copies the secret or match into the message.
func parseGitleaks(o Output, g *Gate) ([]finding.Finding, error) {
	var r []struct {
		RuleID, Description, File string
		StartLine, StartColumn    int
	}
	if err := decodeFirst(o.Stdout, &r); err != nil {
		return nil, err
	}
	out := make([]finding.Finding, 0, len(r))
	for _, l := range r {
		out = append(out, finding.Finding{
			Rule: l.RuleID, Severity: severity("", defSev(g)), File: l.File, Line: l.StartLine, Col: l.StartColumn,
			Message: "possible secret (" + l.RuleID + ")", FixHint: "remove it and rotate the credential; record only where it lives",
		})
	}
	return out, nil
}

type shellcheckComment struct {
	File    string
	Level   string
	Message string
	Line    int
	Column  int
	Code    int
}

// parseShellcheck accepts both -f json (array) and -f json1 ({comments}).
func parseShellcheck(o Output, g *Gate) ([]finding.Finding, error) {
	b := bytes.TrimSpace(o.Stdout)
	var cs []shellcheckComment
	if bytes.HasPrefix(b, []byte("{")) {
		var w struct{ Comments []shellcheckComment }
		if err := decodeFirst(b, &w); err != nil {
			return nil, err
		}
		cs = w.Comments
	} else if err := decodeFirst(b, &cs); err != nil {
		return nil, err
	}
	out := make([]finding.Finding, 0, len(cs))
	for _, c := range cs {
		out = append(out, finding.Finding{Rule: fmt.Sprintf("SC%d", c.Code), Severity: severity(c.Level, defSev(g)), File: c.File, Line: c.Line, Col: c.Column, Message: c.Message})
	}
	return out, nil
}

// parseClippy reads cargo's --message-format=json stream.
func parseClippy(o Output, g *Gate) ([]finding.Finding, error) {
	var out []finding.Finding
	sc := bufio.NewScanner(bytes.NewReader(o.Stdout))
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var ev struct {
			Reason  string `json:"reason"`
			Message *struct {
				Code *struct {
					Code string `json:"code"`
				} `json:"code"`
				Level   string `json:"level"`
				Message string `json:"message"`
				Spans   []struct {
					FileName    string `json:"file_name"`
					LineStart   int    `json:"line_start"`
					ColumnStart int    `json:"column_start"`
					IsPrimary   bool   `json:"is_primary"`
				} `json:"spans"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil || ev.Reason != "compiler-message" || ev.Message == nil || len(ev.Message.Spans) == 0 {
			continue
		}
		m := ev.Message
		f := finding.Finding{Rule: "rustc", Severity: severity(m.Level, defSev(g)), Message: m.Message}
		if m.Code != nil {
			f.Rule = m.Code.Code
		}
		span := m.Spans[0]
		for _, s := range m.Spans {
			if s.IsPrimary {
				span = s
				break
			}
		}
		f.File, f.Line, f.Col = span.FileName, span.LineStart, span.ColumnStart
		out = append(out, f)
	}
	return out, nil
}

var goTestLoc = regexp.MustCompile(`^\s*([\w./-]+\.go):(\d+): ?(.*)$`)

// parseGoTest turns failed tests (and failed packages without failing
// tests, e.g. build errors) from `go test -json` into findings.
func parseGoTest(o Output, g *Gate) ([]finding.Finding, error) {
	type key struct{ pkg, test string }
	outputs := map[key][]string{}
	var failed []key
	failedTests := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(o.Stdout))
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var ev struct{ Action, Package, Test, Output string }
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		k := key{ev.Package, ev.Test}
		switch ev.Action {
		case "output":
			outputs[k] = append(outputs[k], ev.Output)
		case "fail":
			failed = append(failed, k)
			if ev.Test != "" {
				failedTests[ev.Package] = true
			}
		}
	}
	var out []finding.Finding
	for _, k := range failed {
		if k.test == "" && failedTests[k.pkg] {
			continue // the package failed because its tests did
		}
		out = append(out, goTestFinding(k.pkg, k.test, outputs[k], g))
	}
	return out, nil
}

func goTestFinding(pkg, test string, lines []string, g *Gate) finding.Finding {
	f := finding.Finding{Rule: test, Severity: severity("", defSev(g)), File: pkg, Message: "package failed"}
	if test != "" {
		f.Message = test + " failed"
	} else {
		f.Rule = "build"
	}
	for _, l := range lines {
		if m := goTestLoc.FindStringSubmatch(strings.TrimRight(l, "\n")); m != nil {
			f.Line, _ = strconv.Atoi(m[2])
			f.File = strings.TrimSuffix(pkg, "/") + "/" + m[1]
			if msg := strings.TrimSpace(m[3]); msg != "" {
				f.Message = test + ": " + msg
			}
			break
		}
	}
	return f
}

// DefaultLinesRegex matches `file:line[:col]: [severity:] message`.
const DefaultLinesRegex = `^(?P<file>[^:\s][^:]*):(?P<line>\d+)(?::(?P<col>\d+))?:\s*(?:(?P<severity>error|warning|info|note):\s*)?(?P<message>.*)$`

func compileLines(expr string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("lines_regex: %w", err)
	}
	if re.SubexpIndex("file") < 0 {
		return nil, errors.New("lines_regex needs a (?P<file>…) group")
	}
	return re, nil
}

// parseLines applies a regex with named groups file, line, col, message,
// rule and severity to each output line (stdout then stderr).
func parseLines(o Output, g *Gate) ([]finding.Finding, error) {
	expr := DefaultLinesRegex
	if g != nil && g.LinesRegex != "" {
		expr = g.LinesRegex
	}
	re, err := compileLines(expr)
	if err != nil {
		return nil, err
	}
	var out []finding.Finding
	for _, l := range strings.Split(string(o.Stdout)+"\n"+string(o.Stderr), "\n") {
		m := re.FindStringSubmatch(strings.TrimRight(l, "\r"))
		if m == nil {
			continue
		}
		get := func(name string) string {
			if i := re.SubexpIndex(name); i >= 0 {
				return m[i]
			}
			return ""
		}
		f := finding.Finding{File: get("file"), Rule: get("rule"), Message: strings.TrimSpace(get("message")), Severity: severity(get("severity"), defSev(g))}
		f.Line, _ = strconv.Atoi(get("line"))
		f.Col, _ = strconv.Atoi(get("col"))
		if f.Message == "" && g != nil {
			f.Message = g.Desc
		}
		out = append(out, f)
	}
	return out, nil
}

// parseExitcode yields one finding with the (trimmed) output on failure.
func parseExitcode(o Output, g *Gate) ([]finding.Finding, error) {
	if o.Exit == 0 {
		return nil, nil
	}
	text := strings.TrimSpace(string(o.Stdout) + "\n" + string(o.Stderr))
	lines := strings.Split(text, "\n")
	if len(lines) > 20 {
		lines = append(lines[:20], fmt.Sprintf("… %d more lines", len(lines)-20))
	}
	msg := fmt.Sprintf("exit status %d", o.Exit)
	if text != "" {
		msg += ": " + strings.Join(lines, "\n    ")
	}
	return []finding.Finding{{Rule: "exit", Severity: severity("", defSev(g)), Message: msg}}, nil
}
