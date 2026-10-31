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
	fmt.Fprintln(u.out, u.paint("36", "╰"+strings.Repeat("─", 58)))
	yesNo := len(options) == 2 && strings.EqualFold(strings.TrimSpace(options[0]), "yes") && strings.EqualFold(strings.TrimSpace(options[1]), "no")
	prompt := "Choose 1–" + strconv.Itoa(len(options))
	if yesNo {
		prompt += " (y/n)"
	}
	if comment {
		prompt += " or type a comment"
	}
	for {
		fmt.Fprint(u.out, u.paint("36", "› ")+prompt+": ")
		line, err := u.in.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(u.out)
			return "", false
		}
		answer := strings.TrimSpace(line)
		choice, parseErr := strconv.Atoi(answer)
		if parseErr == nil && choice >= 1 && choice <= len(options) {
			fmt.Fprintln(u.out, u.paint("32", "  ✓")+" "+options[choice-1])
			return options[choice-1], true
		}
		if yesNo {
			switch strings.ToLower(answer) {
			case "y", "yes":
				fmt.Fprintln(u.out, u.paint("32", "  ✓")+" Yes")
				return options[0], true
			case "n", "no":
				fmt.Fprintln(u.out, u.paint("32", "  ✓")+" No")
				return options[1], true
			}
		}
		if comment && answer != "" && parseErr != nil {
			fmt.Fprintln(u.out, u.paint("32", "  ✓")+" Comment sent")
			return answer, true
		}
		hint := "Choose a listed number."
		if comment {
			hint = "Choose a listed number or type a comment."
		}
		fmt.Fprintln(u.out, u.paint("33", hint))
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
		fmt.Fprintln(u.out, detail)
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
