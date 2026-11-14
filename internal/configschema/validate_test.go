package configschema

import (
	"strings"
	"testing"
)

const testSchema = `
type: object
additionalProperties: false
required: [name, count]
properties:
  name: {type: string}
  count: {type: integer}
`

func TestValidationMessageListsEveryError(t *testing.T) {
	result, err := validateConfiguration("count: two\nextra: 1\n", testSchema)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid() {
		t.Fatal("want invalid")
	}
	msg := validationMessage(result)
	for _, want := range []string{"name", "count", "extra"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %s", msg, want)
		}
	}
}

func TestValidationMessageValid(t *testing.T) {
	result, err := validateConfiguration("name: a\ncount: 2\n", testSchema)
	if err != nil {
		t.Fatal(err)
	}
	if got := validationMessage(result); got != "The configuration is valid!" {
		t.Fatalf("got %q", got)
	}
}
