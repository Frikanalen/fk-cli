package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
	"golang.org/x/term"
)

// Video records go in and out of the CLI as JSON or YAML. Input is always
// parsed as YAML, which JSON is a subset of, so every command that reads a
// record takes either without being told which.

// readOnlyVideoFields are the fields of a video record the API sends but
// does not take back. They are left out of what apply and edit send, and a
// change to one is reported rather than silently dropped.
var readOnlyVideoFields = map[string]string{
	"id":           "",
	"files":        "",
	"creator":      "",
	"organization": "",
	"series":       "set seriesId to move the video to another series",
	"durationSec":  "",
	"createdTime":  "",
	"updatedTime":  "",
	"isBanned":     "",
	"liveAirings":  "",
}

// writeOnlyVideoFields are fields the API takes but never shows, so they
// cannot be compared with the video as it stands and are always sent.
var writeOnlyVideoFields = map[string]bool{"seriesId": true}

// readInput reads the file named in rest, or stdin when rest is empty or
// names "-". It refuses to wait on a terminal, where a forgotten pipe would
// otherwise look like a hang.
func readInput(cmd *cobra.Command, rest []string) ([]byte, error) {
	if len(rest) == 1 && rest[0] != "-" {
		return os.ReadFile(rest[0])
	}
	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return nil, usageErrorf("no record given: name a file, or pipe JSON or YAML in on stdin")
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return nil, fmt.Errorf("reading stdin: %w", err)
	}
	return data, nil
}

// parseDocuments reads every YAML document in data (JSON being one such
// document). A top-level list counts as one document per item, so a JSON
// array and a "---"-separated YAML stream mean the same thing.
func parseDocuments(data []byte) ([]any, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []any
	for {
		var node yaml.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("the record is neither valid JSON nor YAML: %w", err)
		}
		value, err := nodeValue(&node)
		if err != nil {
			return nil, err
		}
		switch v := value.(type) {
		case nil:
			// An empty document, e.g. a trailing "---".
		case []any:
			docs = append(docs, v...)
		default:
			docs = append(docs, v)
		}
	}
}

// parseObject reads exactly one JSON or YAML object.
func parseObject(data []byte) (map[string]any, error) {
	docs, err := parseDocuments(data)
	if err != nil {
		return nil, err
	}
	if len(docs) != 1 {
		return nil, fmt.Errorf("expected one video record, got %d", len(docs))
	}
	obj, ok := docs[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("a video record must be an object of fields")
	}
	return obj, nil
}

// nodeValue turns a YAML node into the plain values encoding/json marshals.
// Unlike decoding straight into an interface, timestamps stay the strings
// they were written as instead of becoming time.Time.
func nodeValue(node *yaml.Node) (any, error) {
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil, nil
		}
		return nodeValue(node.Content[0])
	case yaml.AliasNode:
		return nodeValue(node.Alias)
	case yaml.MappingNode:
		obj := make(map[string]any, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("line %d: field names must be plain strings", key.Line)
			}
			value, err := nodeValue(node.Content[i+1])
			if err != nil {
				return nil, err
			}
			obj[key.Value] = value
		}
		return obj, nil
	case yaml.SequenceNode:
		list := make([]any, 0, len(node.Content))
		for _, item := range node.Content {
			value, err := nodeValue(item)
			if err != nil {
				return nil, err
			}
			list = append(list, value)
		}
		return list, nil
	case yaml.ScalarNode:
		if node.ShortTag() == "!!timestamp" || node.ShortTag() == "!!binary" {
			return node.Value, nil
		}
		var value any
		if err := node.Decode(&value); err != nil {
			return nil, fmt.Errorf("line %d: %w", node.Line, err)
		}
		return value, nil
	}
	return nil, fmt.Errorf("line %d: unexpected YAML", node.Line)
}

// toYAML renders a JSON document as block-style YAML, keeping the field
// order the API sent and writing multi-line text as literal blocks, which
// is the form that is pleasant to edit.
func toYAML(raw []byte) ([]byte, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return nil, err
	}
	var restyle func(*yaml.Node)
	restyle = func(n *yaml.Node) {
		n.Style = 0
		if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" && strings.Contains(n.Value, "\n") {
			// The encoder falls back to quoting text a literal block
			// cannot hold, such as a carriage return.
			n.Style = yaml.LiteralStyle
		}
		for _, child := range n.Content {
			restyle(child)
		}
	}
	restyle(&node)

	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// recordFormat is how a command prints a video record: --json or --yaml.
type recordFormat struct {
	json bool
	yaml bool
}

func (f *recordFormat) addFlags(cmd *cobra.Command, jsonHelp string) {
	cmd.Flags().BoolVar(&f.json, "json", false, jsonHelp)
	cmd.Flags().BoolVar(&f.yaml, "yaml", false, "Print the record as YAML")
	cmd.MarkFlagsMutuallyExclusive("json", "yaml")
}

// printRecord writes a record the API returned, as YAML or indented JSON.
func printRecord(w io.Writer, raw json.RawMessage, asYAML bool) error {
	if asYAML {
		out, err := toYAML(raw)
		if err != nil {
			return err
		}
		_, err = w.Write(out)
		return err
	}
	return printJSON(w, raw)
}

// fieldChanges compares the fields of a desired record with the video as it
// stands. changed holds the writable fields whose value differs; ignored
// names read-only fields the desired record tried to change, and fields a
// video does not have (most likely typos, which the API would silently
// drop). Fields the desired record leaves out are not touched, as with a
// patch.
func fieldChanges(current, desired map[string]any) (changed map[string]any, ignored []string) {
	changed = map[string]any{}
	for key, want := range desired {
		if writeOnlyVideoFields[key] {
			changed[key] = want
			continue
		}
		have, present := current[key]
		if !present {
			ignored = append(ignored, key)
			continue
		}
		same := sameValue(have, want)
		if _, readOnly := readOnlyVideoFields[key]; readOnly {
			if !same {
				ignored = append(ignored, key)
			}
			continue
		}
		if !same {
			changed[key] = want
		}
	}
	sort.Strings(ignored)
	return changed, ignored
}

// sameValue compares a field as the API holds it with the value asked for.
// Text is compared without surrounding whitespace, which the API strips on
// the way in: otherwise a YAML block, which ends in a newline, would never
// match what was stored from it.
func sameValue(have, want any) bool {
	h, hText := have.(string)
	w, wText := want.(string)
	if hText && wText {
		return strings.TrimSpace(h) == strings.TrimSpace(w)
	}
	return sameJSON(have, want)
}

// sameJSON compares two values by their JSON form, so 25000 read from YAML
// equals 25000.0 read from JSON.
func sameJSON(a, b any) bool {
	x, errX := json.Marshal(a)
	y, errY := json.Marshal(b)
	return errX == nil && errY == nil && bytes.Equal(x, y)
}

// sortedKeys lists the fields of a change, for messages.
func sortedKeys(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// warnIgnored tells the user, on stderr, which read-only fields were left
// alone and how to make the change they probably meant.
func warnIgnored(w io.Writer, videoId int, ignored []string) {
	for _, key := range ignored {
		hint, readOnly := readOnlyVideoFields[key]
		if !readOnly {
			fmt.Fprintf(w, "fk: video %d: a video has no field %q, ignoring it\n", videoId, key)
			continue
		}
		msg := fmt.Sprintf("fk: video %d: %s is read-only, ignoring it", videoId, key)
		if hint != "" {
			msg += " (" + hint + ")"
		}
		fmt.Fprintln(w, msg)
	}
}
