package verify

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type counter struct {
	File    string `json:"file"`
	Metric  string `json:"metric"`
	Covered int    `json:"covered"`
	Total   int    `json:"total"`
}
type metrics struct {
	Files    []string  `json:"files"`
	Counters []counter `json:"counters"`
	Notes    []string  `json:"notes"`
}

func scopedPath(name, root string, scope []string) (string, error) {
	name = filepath.ToSlash(filepath.Clean(name))
	root = filepath.ToSlash(root)
	if strings.HasPrefix(name, root+"/") {
		name = strings.TrimPrefix(name, root+"/")
	}
	found := ""
	for _, p := range scope {
		p = filepath.ToSlash(p)
		if name == p || strings.HasSuffix(p, "/"+name) {
			if found != "" && found != p {
				return "", fmt.Errorf("ambiguous report path %s", name)
			}
			found = p
		}
	}
	return found, nil
}
func parseMetrics(data []byte, format, root string, scope []string) (metrics, error) {
	m := metrics{Files: []string{}, Counters: []counter{}, Notes: []string{}}
	seen := map[string]bool{}
	add := func(name, metric string, covered, total int) error {
		p, err := scopedPath(name, root, scope)
		if err != nil {
			return err
		}
		if p == "" {
			return nil
		}
		if covered < 0 || total < 0 || covered > total {
			return errors.New("invalid report counters")
		}
		if total == 0 {
			return nil
		}
		key := p + ":" + metric
		if seen[key] {
			return fmt.Errorf("duplicate measurement for %s", key)
		}
		seen[key] = true
		m.Counters = append(m.Counters, counter{p, metric, covered, total})
		m.Files = append(m.Files, p)
		return nil
	}
	switch format {
	case "istanbul":
		var report map[string]struct {
			Path string           `json:"path"`
			S    map[string]int   `json:"s"`
			B    map[string][]int `json:"b"`
		}
		if err := json.Unmarshal(data, &report); err != nil {
			return m, err
		}
		for name, f := range report {
			if f.Path != "" {
				name = f.Path
			}
			covered := 0
			for _, n := range f.S {
				if n < 0 {
					return m, errors.New("negative coverage")
				}
				if n > 0 {
					covered++
				}
			}
			if err := add(name, "statements", covered, len(f.S)); err != nil {
				return m, err
			}
			c, t := 0, 0
			for _, hits := range f.B {
				for _, n := range hits {
					if n < 0 {
						return m, errors.New("negative coverage")
					}
					t++
					if n > 0 {
						c++
					}
				}
			}
			if err := add(name, "branches", c, t); err != nil {
				return m, err
			}
			if t == 0 {
				m.Notes = append(m.Notes, name+": no instrumented branches")
			}
		}
	case "lcov":
		name := ""
		lc, lt, bc, bt := 0, 0, 0, 0
		flush := func() error {
			if err := add(name, "lines", lc, lt); err != nil {
				return err
			}
			if err := add(name, "branches", bc, bt); err != nil {
				return err
			}
			if bt == 0 && name != "" {
				m.Notes = append(m.Notes, name+": branch coverage not reported")
			}
			return nil
		}
		scan := bufio.NewScanner(strings.NewReader(string(data)))
		scan.Buffer(make([]byte, 4096), 1<<20)
		for scan.Scan() {
			line := scan.Text()
			switch {
			case strings.HasPrefix(line, "SF:"):
				if name != "" {
					return m, errors.New("unterminated LCOV record")
				}
				name = line[3:]
			case strings.HasPrefix(line, "DA:"):
				parts := strings.Split(line[3:], ",")
				if len(parts) < 2 {
					return m, errors.New("invalid LCOV line")
				}
				n, e := strconv.Atoi(parts[1])
				if e != nil || n < 0 {
					return m, errors.New("invalid LCOV hits")
				}
				lt++
				if n > 0 {
					lc++
				}
			case strings.HasPrefix(line, "BRDA:"):
				parts := strings.Split(line[5:], ",")
				if len(parts) != 4 {
					return m, errors.New("invalid LCOV branch")
				}
				bt++
				if parts[3] != "-" {
					n, e := strconv.Atoi(parts[3])
					if e != nil || n < 0 {
						return m, errors.New("invalid LCOV branch hits")
					}
					if n > 0 {
						bc++
					}
				}
			case line == "end_of_record":
				if err := flush(); err != nil {
					return m, err
				}
				name = ""
				lc, lt, bc, bt = 0, 0, 0, 0
			}
		}
		if err := scan.Err(); err != nil {
			return m, err
		}
		if name != "" {
			return m, errors.New("unterminated LCOV record")
		}
	case "jacoco":
		type count struct {
			Type    string `xml:"type,attr"`
			Missed  int    `xml:"missed,attr"`
			Covered int    `xml:"covered,attr"`
		}
		var report struct {
			Packages []struct {
				Name    string `xml:"name,attr"`
				Sources []struct {
					Name     string  `xml:"name,attr"`
					Counters []count `xml:"counter"`
				} `xml:"sourcefile"`
			} `xml:"package"`
		}
		if err := xml.Unmarshal(data, &report); err != nil {
			return m, err
		}
		for _, pkg := range report.Packages {
			for _, f := range pkg.Sources {
				for _, c := range f.Counters {
					if c.Type == "LINE" || c.Type == "BRANCH" {
						if err := add(strings.TrimPrefix(pkg.Name+"/"+f.Name, "/"), strings.ToLower(c.Type), c.Covered, c.Covered+c.Missed); err != nil {
							return m, err
						}
					}
				}
			}
		}
	case "cobertura":
		var report struct {
			Classes []struct {
				Filename string `xml:"filename,attr"`
				Lines    []struct {
					Hits      int    `xml:"hits,attr"`
					Branch    string `xml:"branch,attr"`
					Condition string `xml:"condition-coverage,attr"`
				} `xml:"lines>line"`
			} `xml:"packages>package>classes>class"`
		}
		if err := xml.Unmarshal(data, &report); err != nil {
			return m, err
		}
		pattern := regexp.MustCompile(`\((\d+)/(\d+)\)`)
		for _, f := range report.Classes {
			c, bc, bt := 0, 0, 0
			for _, line := range f.Lines {
				if line.Hits < 0 {
					return m, errors.New("negative coverage")
				}
				if line.Hits > 0 {
					c++
				}
				if line.Branch == "true" {
					match := pattern.FindStringSubmatch(line.Condition)
					if len(match) != 3 {
						return m, errors.New("missing Cobertura branch counters")
					}
					a, _ := strconv.Atoi(match[1])
					b, _ := strconv.Atoi(match[2])
					bc += a
					bt += b
				}
			}
			if err := add(f.Filename, "lines", c, len(f.Lines)); err != nil {
				return m, err
			}
			if err := add(f.Filename, "branches", bc, bt); err != nil {
				return m, err
			}
			if bt == 0 {
				m.Notes = append(m.Notes, f.Filename+": no branch counters reported")
			}
		}
	default:
		return m, fmt.Errorf("unsupported native report format %q", format)
	}
	m.Files = unique(m.Files)
	for _, p := range scope {
		if !contains(m.Files, p) {
			return m, fmt.Errorf("report has no measurable data for scoped file %s", p)
		}
		primary := false
		for _, c := range m.Counters {
			if c.File == p && (c.Metric == "lines" || c.Metric == "line" || c.Metric == "statements") {
				primary = true
			}
		}
		if !primary {
			return m, fmt.Errorf("report is missing primary coverage counters for %s", p)
		}
	}
	if len(scope) > 0 && len(m.Counters) == 0 {
		return m, errors.New("empty measurement")
	}
	return m, nil
}
func contains(items []string, s string) bool {
	for _, v := range items {
		if v == s {
			return true
		}
	}
	return false
}
