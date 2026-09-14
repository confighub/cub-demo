package present

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/confighub/cub-demo/internal/cubexec"
)

// The pipeline and delivery-bot behaviors that used to be verbs here (CI,
// argobot) are now plays a scenario composes from the primitives in steps.go.

// currentTag reads the tag the named container runs in the base's unit.
func (p *Presenter) currentTag(space, unit, container string) (string, error) {
	data, err := cubexec.Run("unit", "data", unit, "--space", space)
	if err != nil {
		return "", err
	}
	dec := yaml.NewDecoder(strings.NewReader(data))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			break
		}
		if image := findContainerImage(doc, container); image != "" {
			if i := strings.LastIndex(image, ":"); i > 0 && !strings.Contains(image[i:], "/") {
				return image[i+1:], nil
			}
			return "", fmt.Errorf("%s/%s: container %s image %q carries no tag", space, unit, container, image)
		}
	}
	return "", fmt.Errorf("%s/%s: no container named %s", space, unit, container)
}

func findContainerImage(node any, container string) string {
	switch v := node.(type) {
	case map[string]any:
		if name, _ := v["name"].(string); name == container {
			if image, _ := v["image"].(string); image != "" {
				return image
			}
		}
		for _, child := range v {
			if image := findContainerImage(child, container); image != "" {
				return image
			}
		}
	case []any:
		for _, child := range v {
			if image := findContainerImage(child, container); image != "" {
				return image
			}
		}
	}
	return ""
}

// bump raises a dotted version by one at the named position, keeping a "v"
// prefix if the tag has one and resetting the positions after it.
func bump(tag, how string) (string, error) {
	prefix := ""
	rest := tag
	if strings.HasPrefix(rest, "v") {
		prefix, rest = "v", rest[1:]
	}
	parts := strings.Split(rest, ".")
	nums := make([]int, len(parts))
	for i, s := range parts {
		n, err := strconv.Atoi(s)
		if err != nil {
			return "", fmt.Errorf("cannot bump tag %q: %q is not a number", tag, s)
		}
		nums[i] = n
	}
	var at int
	switch how {
	case "minor", "":
		at = 1
	case "patch":
		at = 2
	case "major":
		at = 0
	default:
		return "", fmt.Errorf("--bump %q: want major, minor or patch", how)
	}
	if at >= len(nums) {
		return "", fmt.Errorf("cannot bump %s of tag %q: it has %d parts", how, tag, len(nums))
	}
	nums[at]++
	for i := at + 1; i < len(nums); i++ {
		nums[i] = 0
	}
	out := make([]string, len(nums))
	for i, n := range nums {
		out[i] = strconv.Itoa(n)
	}
	return prefix + strings.Join(out, "."), nil
}

func shellJoin(args []string) string {
	q := make([]string, 0, len(args))
	for _, a := range args {
		if strings.ContainsAny(a, " \t\"'$") {
			a = "\"" + strings.ReplaceAll(a, "\"", "\\\"") + "\""
		}
		q = append(q, a)
	}
	return strings.Join(q, " ")
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}
