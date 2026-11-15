package web

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// parseIDSelection collects every form key of the form "<prefix><id>" into a
// slice of ids. Checkbox groups ("arch-5", "arch-12", ...) are submitted
// this way because HTML forms don't have a native multi-value-per-name
// checkbox-group idiom that survives being named identically.
func parseIDSelection(form url.Values, prefix string) ([]int, error) {
	var ids []int
	for k := range form {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		id, err := strconv.Atoi(strings.TrimPrefix(k, prefix))
		if err != nil {
			return nil, fmt.Errorf("invalid id in %q: %w", k, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func parseArchBitSelection(form url.Values) (archIDs, bitIDs []int, err error) {
	archIDs, err = parseIDSelection(form, "arch-")
	if err != nil {
		return nil, nil, err
	}
	bitIDs, err = parseIDSelection(form, "bit-")
	if err != nil {
		return nil, nil, err
	}
	return archIDs, bitIDs, nil
}

// validateSingleArchBit enforces that a bot has exactly one arch and one bit
// selected - a bot's assembly is only valid against a single ISA/word size.
func validateSingleArchBit(archIDs, bitIDs []int) (msg string, ok bool) {
	switch {
	case len(archIDs) == 0:
		return "ERROR: Please select an architecture", false
	case len(archIDs) >= 2:
		return "ERROR: Please select ONE architecture", false
	case len(bitIDs) == 0:
		return "ERROR: Please select one of the bits", false
	case len(bitIDs) >= 2:
		return "ERROR: Please select ONE of the bits", false
	default:
		return "", true
	}
}
