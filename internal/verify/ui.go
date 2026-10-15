package verify

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type terminalUI struct {
	in       *bufio.Reader
	out      io.Writer
	terminal bool
	color    bool
}

func newTerminalUI(in io.Reader, out io.Writer) *terminalUI {
	terminal := false
	if file, ok := out.(*os.File); ok {
		if info, err := file.Stat(); err == nil {
			terminal = info.Mode()&os.ModeCharDevice != 0
		}
	}
	color := terminal && os.Getenv("TERM") != "dumb" && os.Getenv("NO_COLOR") == ""
	return &terminalUI{in: bufio.NewReader(in), out: out, terminal: terminal, color: color}
}

func (u *terminalUI) paint(code, s string) string {
	if !u.color || code == "" || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (u *terminalUI) stage(number, title string) {
	fmt.Fprintln(u.out, "\n"+u.paint("1;36", "VERIFY  "+number+" / 4")+"  "+u.paint("1", title))
	fmt.Fprintln(u.out, u.paint("2", strings.Repeat("─", 58)))
}

func (u *terminalUI) activity(message string) {
	fmt.Fprintln(u.out, u.paint("36", "  •")+" "+message)
}

func (u *terminalUI) note(label, value string) {
	fmt.Fprintln(u.out, u.paint("1", label)+"  "+value)
}

func (u *terminalUI) problem(message string) {
	fmt.Fprintln(u.out, u.paint("1;31", "  !")+" "+message)
}

// choose returns the selected label, or the user's own comment. EOF leaves
// the decision pending, which is also how noninteractive mode behaves.
func (u *terminalUI) choose(question string, options []string, comment bool) (string, bool) {
	if len(options) == 0 {
		return "", false
	}
	fmt.Fprintln(u.out, "\n"+u.paint("36", "╭─ DECISION"))
	for _, line := range wrapWords(question, 72) {
		fmt.Fprintln(u.out, u.paint("36", "│ ")+u.paint("1", line))
	}
	fmt.Fprintln(u.out, u.paint("36", "│"))
	for i, option := range options {
		lines := wrapWords(option, 66)
		for j, line := range lines {
			if j == 0 {
				fmt.Fprintf(u.out, "%s  %s  %s\n", u.paint("36", "│"), u.paint("36", strconv.Itoa(i+1)+"."), line)
			} else {
				fmt.Fprintln(u.out, u.paint("36", "│")+"      "+line)
			}
		}
	}
	if comment {
		fmt.Fprintf(u.out, "%s  %s  Add a comment\n", u.paint("36", "│"), u.paint("36", strconv.Itoa(len(options)+1)+"."))
	}
	fmt.Fprintln(u.out, u.paint("36", "╰"+strings.Repeat("─", 58)))
	count := len(options)
	if comment {
		count++
	}
	for {
		fmt.Fprint(u.out, u.paint("36", "› ")+"Choose 1–"+strconv.Itoa(count)+": ")
		line, err := u.in.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(u.out)
			return "", false
		}
		choice, parseErr := strconv.Atoi(strings.TrimSpace(line))
		if parseErr == nil && choice >= 1 && choice <= count {
			if comment && choice == count {
				for {
					fmt.Fprint(u.out, u.paint("36", "› ")+"Your comment: ")
					answer, readErr := u.in.ReadString('\n')
					if readErr != nil && answer == "" {
						fmt.Fprintln(u.out)
						return "", false
					}
					if answer = strings.TrimSpace(answer); answer != "" {
						fmt.Fprintln(u.out, u.paint("32", "  ✓")+" Comment sent")
						return answer, true
					}
					fmt.Fprintln(u.out, u.paint("33", "Please enter a comment."))
				}
			}
			fmt.Fprintln(u.out, u.paint("32", "  ✓")+" "+options[choice-1])
			return options[choice-1], true
		}
		fmt.Fprintln(u.out, u.paint("33", "Choose a number from the list."))
	}
}

func wrapWords(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	lines := []string{}
	line := ""
	for _, word := range words {
		if line != "" && len([]rune(line))+1+len([]rune(word)) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	return append(lines, line)
}

func (u *terminalUI) preview(title, detail, diff string) {
	fmt.Fprintln(u.out, "\n"+u.paint("1;36", title))
	if detail != "" {
		u.report(detail)
	}
	for _, line := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
		code := ""
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "diff --git"):
			code = "1"
		case strings.HasPrefix(line, "+"):
			code = "32"
		case strings.HasPrefix(line, "-"):
			code = "31"
		case strings.HasPrefix(line, "@@"):
			code = "36"
		case strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "\\ No newline"):
			code = "2"
		}
		fmt.Fprintln(u.out, u.paint(code, line))
	}
}

func (u *terminalUI) report(markdown string) {
	lines := strings.Split(strings.TrimSpace(markdown), "\n")
	inCode := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			inCode = !inCode
			continue
		case inCode:
			fmt.Fprintln(u.out, u.paint("2", "  │ ")+line)
		case strings.HasPrefix(trimmed, "# "):
			tone := "1;33"
			if strings.Contains(strings.ToLower(trimmed), "good to go") {
				tone = "1;32"
			}
			fmt.Fprintln(u.out, "\n"+u.paint(tone, strings.TrimPrefix(trimmed, "# ")))
			fmt.Fprintln(u.out, u.paint("2", strings.Repeat("─", 58)))
		case strings.HasPrefix(trimmed, "## "):
			fmt.Fprintln(u.out, "\n"+u.paint("1;36", strings.TrimPrefix(trimmed, "## ")))
		case strings.HasPrefix(trimmed, "### "):
			fmt.Fprintln(u.out, "\n"+u.paint("1", strings.TrimPrefix(trimmed, "### ")))
		case strings.HasPrefix(trimmed, "**Verdict:**"):
			verdict := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "**Verdict:**")), "`")
			tone, mark := "1;33", "!"
			if verdict == "good_to_go" {
				tone, mark = "1;32", "✓"
			} else if verdict == "not_ready" {
				tone = "1;31"
			}
			fmt.Fprintln(u.out, "\n"+u.paint(tone, mark+"  VERDICT  "+strings.ReplaceAll(verdict, "_", " ")))
		case trimmed == "---":
			fmt.Fprintln(u.out, u.paint("2", strings.Repeat("─", 58)))
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			fmt.Fprintln(u.out, "  "+u.paint("36", "•")+" "+u.inline(trimmed[2:]))
		case strings.HasPrefix(trimmed, "|"):
			end := i
			for end < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[end]), "|") {
				end++
			}
			if end-i > 1 {
				u.table(lines[i:end])
				i = end - 1
			} else {
				fmt.Fprintln(u.out, u.inline(trimmed))
			}
		default:
			fmt.Fprintln(u.out, u.inline(line))
		}
	}
}

func (u *terminalUI) table(lines []string) {
	rows := make([][]string, 0, len(lines))
	widths := []int{}
	for _, line := range lines {
		if strings.Trim(strings.TrimSpace(line), "|-: ") == "" {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
			if j >= len(widths) {
				widths = append(widths, 0)
			}
			if width := len([]rune(cells[j])); width > widths[j] {
				widths[j] = width
			}
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return
	}
	totalWidth := len(widths) + 1
	for _, width := range widths {
		totalWidth += width + 2
	}
	if totalWidth > 100 && len(rows) > 1 {
		for _, row := range rows[1:] {
			if len(row) == 0 {
				continue
			}
			fmt.Fprintln(u.out, "  "+u.paint("1;36", row[0]))
			for j := 1; j < len(row) && j < len(rows[0]); j++ {
				value := strings.Trim(row[j], "`")
				for k, part := range wrapWords(value, 68) {
					if k == 0 {
						fmt.Fprintln(u.out, "    "+u.paint("2", rows[0][j]+": ")+part)
					} else {
						fmt.Fprintln(u.out, "      "+part)
					}
				}
			}
		}
		return
	}
	border := func(left, middle, right string) string {
		parts := make([]string, len(widths))
		for j, width := range widths {
			parts[j] = strings.Repeat("─", width+2)
		}
		return left + strings.Join(parts, middle) + right
	}
	fmt.Fprintln(u.out, u.paint("2", border("┌", "┬", "┐")))
	for i, cells := range rows {
		parts := make([]string, len(widths))
		for j, width := range widths {
			value := ""
			if j < len(cells) {
				value = cells[j]
			}
			padding := strings.Repeat(" ", width-len([]rune(value)))
			if i == 0 {
				value = u.paint("1", u.inline(value))
			} else {
				value = u.inline(value)
			}
			parts[j] = " " + value + padding + " "
		}
		fmt.Fprintln(u.out, u.paint("2", "│")+strings.Join(parts, u.paint("2", "│"))+u.paint("2", "│"))
		if i == 0 && len(rows) > 1 {
			fmt.Fprintln(u.out, u.paint("2", border("├", "┼", "┤")))
		}
	}
	fmt.Fprintln(u.out, u.paint("2", border("└", "┴", "┘")))
}

func (u *terminalUI) inline(s string) string {
	parts := strings.Split(s, "**")
	for i := 1; i < len(parts); i += 2 {
		parts[i] = u.paint("1", parts[i])
	}
	s = strings.Join(parts, "")
	parts = strings.Split(s, "`")
	for i := 1; i < len(parts); i += 2 {
		parts[i] = u.paint("36", parts[i])
	}
	return strings.Join(parts, "")
}
