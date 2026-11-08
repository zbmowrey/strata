package knowledge

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/zbmowrey/strata/engine/internal/corpus"
	"github.com/zbmowrey/strata/engine/internal/doc"
	"github.com/zbmowrey/strata/engine/internal/fail"
)

// Question is one escalation file.
type Question struct {
	ID       string   `json:"id"`
	Status   string   `json:"status"`
	RaisedBy string   `json:"raised_by"`
	Raised   string   `json:"raised"`
	Question string   `json:"question"`
	Path     string   `json:"path"`
	Blocking []string `json:"blocking,omitempty"`
}

const answerPlaceholder = "<!-- human writes here; then: strata questions answer "

// Questions lists questions, open ones only unless all is set, by id.
func (s *Store) Questions(all bool) ([]Question, error) {
	fs, err := s.files()
	if err != nil {
		return nil, err
	}
	var out []Question
	for _, f := range fs {
		if f.Kind != corpus.Question {
			continue
		}
		q, err := s.loadQuestion(f.Rel)
		if err != nil {
			return nil, err
		}
		if all || q.Status == "open" {
			out = append(out, q)
		}
	}
	slices.SortFunc(out, func(a, b Question) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func (s *Store) loadQuestion(rel string) (Question, error) {
	raw, err := s.readFile(rel)
	if err != nil {
		return Question{}, err
	}
	d, _ := doc.Parse(rel, raw)
	q := Question{
		ID: d.Meta.String("id"), Status: d.Meta.String(keyStatus), RaisedBy: d.Meta.String("raised_by"),
		Raised: d.Meta.String("raised"), Blocking: d.Meta.Strings("blocking"), Path: rel,
	}
	if q.ID == "" {
		q.ID = strings.TrimSuffix(path.Base(rel), ".md")
	}
	q.Question = sectionText(d, "Question")
	return q, nil
}

// sectionText returns a section's text without its heading line.
func sectionText(d *doc.Doc, heading string) string {
	h, ok := d.Section(heading)
	if !ok {
		return ""
	}
	_, rest, _ := strings.Cut(string(d.Raw[h.Start:h.End]), "\n")
	return strings.TrimSpace(rest)
}

func (s *Store) questionPath(id string) string {
	return path.Join(s.layout.Knowledge, "_questions", id+".md")
}

// ShowQuestion returns a question's file content.
func (s *Store) ShowQuestion(id string) (string, error) {
	raw, err := s.readFile(s.questionPath(id))
	if err != nil {
		return "", fail.Wrap(fail.Partial, "list questions with `strata questions list --all`", err)
	}
	return string(raw), nil
}

// Ask raises a question (raised_by claude) and returns its path.
func (s *Store) Ask(text string, blocking []string, raisedBy string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fail.New(fail.Usage, `strata questions ask "<question>"`, "empty question")
	}
	if raisedBy == "" {
		raisedBy = "claude"
	}
	id := "q-" + s.now().Format("20060102") + "-" + Slugify(text, 40)
	for i := 2; ; i++ {
		if _, taken, err := s.idTaken(id); err != nil {
			return "", err
		} else if !taken {
			break
		}
		id = fmt.Sprintf("q-%s-%s-%d", s.now().Format("20060102"), Slugify(text, 40), i)
	}
	content := "---\nid: " + id + "\nstatus: open\nraised_by: " + raisedBy + "\nraised: " + s.today() +
		"\nblocking: " + Value(blocking) + "\n---\n## Question\n" + text + "\n## Options\n1. TODO\n## Answer\n" +
		answerPlaceholder + id + " -->\n"
	rel := s.questionPath(id)
	if err := s.writeFile(rel, []byte(content)); err != nil {
		return "", fmt.Errorf("ask: %w", err)
	}
	return rel, nil
}

// Answer marks a question answered once its ## Answer section has content.
func (s *Store) Answer(id string) (string, error) {
	rel := s.questionPath(id)
	raw, err := s.readFile(rel)
	if err != nil {
		return "", fail.Wrap(fail.Partial, "list questions with `strata questions list`", err)
	}
	d, _ := doc.Parse(rel, raw)
	answer := sectionText(d, "Answer")
	if i := strings.Index(answer, answerPlaceholder); i >= 0 {
		answer = strings.TrimSpace(answer[:i] + answer[strings.Index(answer[i:], "-->")+i+3:])
	}
	if answer == "" {
		return "", fail.New(fail.Usage, "write the answer under `## Answer` in "+rel+", then run this again", "question %s has no answer yet", id)
	}
	if _, err := s.edit(rel, func(b []byte) ([]byte, error) {
		return setKeys(b, [2]string{keyStatus, "answered"}, [2]string{"answered", s.today()})
	}); err != nil {
		return "", err
	}
	return rel, nil
}

// Apply marks an answered question applied (the gardener's apply-answer
// task is done), so it stops producing tasks.
func (s *Store) Apply(id string) (string, error) {
	rel := s.questionPath(id)
	raw, err := s.readFile(rel)
	if err != nil {
		return "", fail.Wrap(fail.Partial, "list questions with `strata questions list --all`", err)
	}
	d, _ := doc.Parse(rel, raw)
	if st := d.Meta.String(keyStatus); st != "answered" {
		return "", fail.New(fail.Usage, "only answered questions can be applied", "question %s is %s", id, st)
	}
	if _, err := s.edit(rel, func(b []byte) ([]byte, error) {
		return setKeys(b, [2]string{keyStatus, "applied"}, [2]string{"applied", s.today()})
	}); err != nil {
		return "", err
	}
	return rel, nil
}
