package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	gitignore "github.com/sabhiram/go-gitignore"
	"github.com/sahilm/fuzzy"
)

const maxIndexedFiles = 20000

var errFileLimit = errors.New("file limit")

type filesLoadedMsg struct {
	root  string
	paths []string
	err   error
}

// fileHit is one row in the @ picker.
// indexes are byte offsets of matched runes, which is what sahilm/fuzzy reports.
type fileHit struct {
	path    string
	indexes []int
}

// mention is the @token that contains the cursor.
type mention struct {
	key   string
	query string
	at    int
	end   int
}

func (m *model) loadFiles() tea.Cmd {
	if m.filesLoading || m.filesReady {
		return nil
	}
	m.filesLoading = true
	root := m.cwd
	return func() tea.Msg {
		paths, err := listProjectFiles(root)
		if paths == nil {
			paths = []string{}
		}
		return filesLoadedMsg{root: root, paths: paths, err: err}
	}
}

func (m *model) onFiles(msg filesLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.root != m.cwd {
		return m, nil
	}
	m.filesLoading = false
	m.filesReady = true
	m.filesErr = ""
	m.files = msg.paths
	if msg.err != nil {
		m.filesErr = msg.err.Error()
		m.files = []string{}
	}
	m.ranked = false
	if m.filePickerOpen() {
		m.layout()
		m.paint()
	}
	return m, nil
}

func (m *model) trackFiles() tea.Cmd {
	if !m.filePickerOpen() {
		return nil
	}
	return m.loadFiles()
}

func (m *model) mention() (mention, bool) {
	return mentionAt(m.input.Value(), m.input.Line(), m.input.Column())
}

// filePickerOpen reports that the cursor is in an @token that Esc has not hidden.
func (m *model) filePickerOpen() bool {
	if m.mode != modeChat {
		return false
	}
	ment, ok := m.mention()
	if !ok {
		return false
	}
	return m.fileDismiss != ment.key
}

// filePickerArmed is a picker that will take Enter instead of sending.
func (m *model) filePickerArmed() bool {
	if !m.filePickerOpen() {
		return false
	}
	m.syncFileRank()
	if m.filesErr != "" {
		return false
	}
	if !m.filesReady {
		return true
	}
	return len(m.fileHits) > 0
}

func (m *model) syncFileRank() {
	if m.mode != modeChat {
		return
	}
	ment, ok := m.mention()
	if !ok {
		return
	}
	if m.fileDismiss != "" && m.fileDismiss != ment.key {
		m.fileDismiss = ""
	}
	if m.fileDismiss == ment.key {
		return
	}
	if m.ranked && m.rankKey == ment.key && m.fileQuery == ment.query {
		if m.fileCursor >= len(m.fileHits) {
			m.fileCursor = 0
		}
		return
	}
	m.ranked = true
	m.rankKey = ment.key
	m.fileQuery = ment.query
	m.fileCursor = 0
	m.fileHits = rankFiles(m.files, ment.query)
}

func (m *model) moveFile(delta int) bool {
	if !m.filePickerArmed() {
		return false
	}
	if !m.filesReady {
		return true
	}
	next := m.fileCursor + delta
	if next < 0 || next >= len(m.fileHits) {
		return true
	}
	m.fileCursor = next
	m.layout()
	return true
}

func (m *model) insertFile() bool {
	if !m.filePickerArmed() {
		return false
	}
	if !m.filesReady {
		return true
	}
	ment, ok := m.mention()
	if !ok || m.fileCursor >= len(m.fileHits) {
		return false
	}
	next, caret := applyMention(m.input.Value(), ment, m.fileHits[m.fileCursor].path)
	setCaret(&m.input, next, caret)
	m.fileDismiss = ""
	m.ranked = false
	m.layout()
	return true
}

func (m *model) dismissFiles() bool {
	if !m.filePickerOpen() {
		return false
	}
	ment, ok := m.mention()
	if !ok {
		return false
	}
	m.fileDismiss = ment.key
	m.layout()
	return true
}

func (m *model) fileOverlay() string {
	if !m.filePickerOpen() {
		return ""
	}
	m.syncFileRank()
	width := m.usableWidth()
	switch {
	case m.filesErr != "":
		return m.theme.bad.Render(ansi.Truncate(m.filesErr, width, "…"))
	case !m.filesReady:
		return m.theme.dim.Render("indexing files")
	case len(m.files) == 0:
		return m.theme.dim.Render("no files")
	case len(m.fileHits) == 0:
		return m.theme.dim.Render("no matching file")
	default:
		return strings.Join(m.fileLines(), "\n")
	}
}

func (m *model) fileLines() []string {
	width := m.usableWidth()
	start, view := pickerWindowSlice(m.fileHits, m.fileCursor)
	lines := make([]string, 0, len(view))
	for i, hit := range view {
		selected := start+i == m.fileCursor
		prefix := "  "
		plain := m.theme.dim
		mark := m.theme.accent
		if selected {
			prefix = "> "
			plain = m.theme.accent
			mark = m.theme.accent.Bold(true)
		}
		line := plain.Render(prefix) + highlightPath(hit.path, hit.indexes, plain, mark)
		lines = append(lines, ansi.Truncate(line, width, "…"))
	}
	return lines
}

func highlightPath(path string, indexes []int, plain, mark lipgloss.Style) string {
	if len(indexes) == 0 {
		return plain.Render(path)
	}
	hot := make(map[int]bool, len(indexes))
	for _, index := range indexes {
		hot[index] = true
	}
	var b strings.Builder
	chunk := []byte{}
	on := false
	flush := func(marked bool) {
		if len(chunk) == 0 {
			return
		}
		style := plain
		if marked {
			style = mark
		}
		b.WriteString(style.Render(string(chunk)))
		chunk = chunk[:0]
	}
	for i := 0; i < len(path); {
		_, size := utf8.DecodeRuneInString(path[i:])
		if size < 1 {
			size = 1
		}
		next := hot[i]
		if len(chunk) > 0 && next != on {
			flush(on)
		}
		on = next
		chunk = append(chunk, path[i:i+size]...)
		i += size
	}
	flush(on)
	return b.String()
}

// rankFiles orders paths for the @ picker.
// An empty query keeps the caller's order. Any other query is scored by
// sahilm/fuzzy, which rewards consecutive characters and matches that follow
// a path separator.
func rankFiles(paths []string, query string) []fileHit {
	if len(paths) == 0 || query == "" {
		hits := make([]fileHit, len(paths))
		for i, path := range paths {
			hits[i] = fileHit{path: path}
		}
		return hits
	}
	matches := fuzzy.Find(query, paths)
	hits := make([]fileHit, len(matches))
	for i, match := range matches {
		indexes := make([]int, len(match.MatchedIndexes))
		copy(indexes, match.MatchedIndexes)
		hits[i] = fileHit{path: match.Str, indexes: indexes}
	}
	return hits
}

// mentionAt finds an @token on line, which is the textarea's cursor row.
// col is the insertion point. It counts when it is inside the token or just
// after it, including before a following space. A token that does not start
// with @, such as name@host, is left alone.
func mentionAt(value string, line, col int) (mention, bool) {
	lines := strings.Split(value, "\n")
	if line < 0 || line >= len(lines) {
		return mention{}, false
	}
	runes := []rune(lines[line])
	if col < 0 {
		col = 0
	}
	if col > len(runes) {
		col = len(runes)
	}
	anchor := col
	if anchor > 0 && (anchor == len(runes) || unicode.IsSpace(runes[anchor])) {
		anchor--
	}
	if anchor < 0 || anchor >= len(runes) || unicode.IsSpace(runes[anchor]) {
		return mention{}, false
	}
	start := anchor
	for start > 0 && !unicode.IsSpace(runes[start-1]) {
		start--
	}
	end := anchor + 1
	for end < len(runes) && !unicode.IsSpace(runes[end]) {
		end++
	}
	if runes[start] != '@' {
		return mention{}, false
	}
	base := runeOffset(lines, line)
	token := string(runes[start:end])
	return mention{
		key:   fmt.Sprintf("%d:%s", line, token),
		query: string(runes[start+1 : end]),
		at:    base + start,
		end:   base + end,
	}, true
}

func runeOffset(lines []string, line int) int {
	off := 0
	for i := 0; i < line; i++ {
		off += len([]rune(lines[i])) + 1
	}
	return off
}

func applyMention(value string, ment mention, file string) (string, int) {
	runes := []rune(value)
	insert := []rune("@" + file)
	// Sit past the separating space so the picker does not reopen on the path.
	gap := 0
	if ment.end >= len(runes) || !unicode.IsSpace(runes[ment.end]) {
		insert = append(insert, ' ')
	} else {
		gap = 1
	}
	next := make([]rune, 0, ment.at+len(insert)+len(runes)-ment.end)
	next = append(next, runes[:ment.at]...)
	next = append(next, insert...)
	caret := len(next) + gap
	next = append(next, runes[ment.end:]...)
	if caret > len(next) {
		caret = len(next)
	}
	return string(next), caret
}

func setCaret(ta *textarea.Model, value string, caret int) {
	ta.SetValue(value)
	line, col := caretPosition(value, caret)
	for guard := 0; ta.Line() > line && guard < len(value)+1; guard++ {
		before := ta.Line()
		ta.CursorUp()
		if ta.Line() == before {
			break
		}
	}
	for guard := 0; ta.Line() < line && guard < len(value)+1; guard++ {
		before := ta.Line()
		ta.CursorDown()
		if ta.Line() == before {
			break
		}
	}
	ta.SetCursorColumn(col)
}

func caretPosition(value string, caret int) (int, int) {
	if caret < 0 {
		return 0, 0
	}
	lines := strings.Split(value, "\n")
	off := 0
	for i, line := range lines {
		n := len([]rune(line))
		if caret <= off+n || i == len(lines)-1 {
			col := caret - off
			if col < 0 {
				col = 0
			}
			if col > n {
				col = n
			}
			return i, col
		}
		off += n + 1
	}
	return 0, 0
}

// listProjectFiles returns slash-separated paths under root.
// It skips .git and anything a .gitignore along the way excludes.
// The walk stops after maxIndexedFiles so a huge tree cannot fill the picker.
func listProjectFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return []string{}, err
	}
	if !info.IsDir() {
		return []string{}, fmt.Errorf("%s is not a directory", root)
	}
	files := []string{}
	var stack ignoreStack
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return nil
		}
		slash := filepath.ToSlash(rel)
		if slash == "." {
			if rules := readIgnore(current); rules != nil {
				stack.push("", rules)
			}
			return nil
		}
		if entry.IsDir() {
			stack.trim(slash)
			if path.Base(slash) == ".git" || stack.ignored(slash, true) {
				return filepath.SkipDir
			}
			if rules := readIgnore(current); rules != nil {
				stack.push(slash, rules)
			}
			return nil
		}
		stack.trim(slash)
		if stack.ignored(slash, false) || !entry.Type().IsRegular() {
			return nil
		}
		files = append(files, slash)
		if len(files) >= maxIndexedFiles {
			return errFileLimit
		}
		return nil
	})
	if err != nil && !errors.Is(err, errFileLimit) {
		return []string{}, err
	}
	slices.Sort(files)
	return files, nil
}

type scopedIgnore struct {
	dir   string
	rules *gitignore.GitIgnore
}

type ignoreStack struct {
	items []scopedIgnore
}

func (s *ignoreStack) push(dir string, rules *gitignore.GitIgnore) {
	s.items = append(s.items, scopedIgnore{dir: dir, rules: rules})
}

func (s *ignoreStack) trim(rel string) {
	for len(s.items) > 0 {
		dir := s.items[len(s.items)-1].dir
		inside := dir == "" || rel == dir || strings.HasPrefix(rel, dir+"/")
		if inside {
			return
		}
		s.items = s.items[:len(s.items)-1]
	}
}

func (s *ignoreStack) ignored(rel string, isDir bool) bool {
	for _, item := range s.items {
		if item.match(rel, isDir) {
			return true
		}
	}
	return false
}

func (g scopedIgnore) match(rel string, isDir bool) bool {
	if g.rules == nil {
		return false
	}
	local := rel
	if g.dir != "" {
		if rel == g.dir {
			return false
		}
		prefix := g.dir + "/"
		if !strings.HasPrefix(rel, prefix) {
			return false
		}
		local = strings.TrimPrefix(rel, prefix)
	}
	if g.rules.MatchesPath(local) {
		return true
	}
	return isDir && g.rules.MatchesPath(local+"/")
}

func readIgnore(dir string) *gitignore.GitIgnore {
	rules, err := gitignore.CompileIgnoreFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	return rules
}
