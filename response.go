package sling

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"unicode/utf8"
)

// ResponseDecoder decodes http responses into struct values.
type ResponseDecoder interface {
	// Decode decodes the response into the value pointed to by v.
	Decode(resp *http.Response, v interface{}) error
}

// jsonDecoder decodes http response JSON into a JSON-tagged struct value.
type jsonDecoder struct {
}

// Decode decodes the Response Body into the value pointed to by v.
// Caller must provide a non-nil v and close the resp.Body.
func (d jsonDecoder) Decode(resp *http.Response, v interface{}) error {
	decoder := json.NewDecoder(resp.Body)

	err := decoder.Decode(v)
	if err != nil {
		return err
	}

	err = validateJSONEnded(decoder, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

func validateJSONEnded(decoder *json.Decoder, r io.Reader) error {
	rest := io.MultiReader(decoder.Buffered(), r)
	reader := bufio.NewReader(rest)

	for {
		b, err := reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				return nil
			}

			return err
		}

		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			var c rune
			if b < utf8.RuneSelf {
				// Anything less than `utf8.RuneSelf` can just be cast to `rune`
				// directly and is guaranteed to be valid.
				c = rune(b)
			} else {
				err := reader.UnreadByte()
				if err != nil {
					panic(fmt.Errorf("invariant violated: UnreadByte shouldn't error when ReadByte directly precedes it, got err: %w", err))
				}

				var size int
				c, size, err = reader.ReadRune()
				if err != nil {
					return fmt.Errorf("could not read unexpected input after top-level JSON: %w", err)
				}

				// `ReadRune` has the special result of `('\uFFFD', 1, nil)`
				// when the input is invalid. '\uFFFD' in this context is called
				// `ut8.RuneError` but is in reality the Unicode replacement
				// character and normally if it is seen in the input it would be
				// `('\uFFFD', 3, nil)`.
				if c == utf8.RuneError && size == 1 {
					return fmt.Errorf("unexpected byte after top-level JSON: %q", b)
				}
			}

			return fmt.Errorf("unexpected character after top-level JSON: %q", c)
		}
	}
}
