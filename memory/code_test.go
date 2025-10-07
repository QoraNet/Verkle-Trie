
package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitAndMerge(t *testing.T) {
	const PUSH2 = 0x61
	const PUSH32 = 0x7f

	tests := map[string]struct {
		code   []byte
		chunks []chunk
	}{
		"empty": {
			code:   []byte{},
			chunks: []chunk{},
		},
		"single chunk": {
			code:   []byte{0x01, 0x02, 0x03},
			chunks: []chunk{{0x00, 0x01, 0x02, 0x03}},
		},
		"multiple chunks": {
			code:   []byte{0x01, 30: 0x02, 31: 0x03, 70: 0x01},
			chunks: []chunk{{0, 0x01, 31: 0x02}, {0, 0x03}, {70%31 + 1: 0x01}},
		},
		"data at boundary": {
			code: []byte{0x01, 0x02, PUSH32, 0x03, 31: 0x04, 80: PUSH2, 0x01},
			chunks: []chunk{
				{0x00, 0x01, 0x02, PUSH32, 0x03},
				{4, 0x04},
				{80%31 + 1: PUSH2, 0x01},
			},
		},
		"code ending with PUSH32": {
			code: []byte{0x03, 30: PUSH32, 62: 0x05},
			chunks: []chunk{
				{0, 0x03, 31: PUSH32},
				{31, 0x00},
				{1, 0x05},
			},
		},
		// In this case the last instruction of the first chunk is a PUSH32.
		// For a valid code, 32 bytes of data (two chunks) should follow.
		// Instead, we only have a single byte (just enough to force the creation of a
		// second chunk). We nevertheless want the entire second chunk to be marked as
		// data.
		"truncated push data at chunk boundary filling full chunk": {
			code: []byte{0x03, 30: PUSH32, 31: 0x05},
			chunks: []chunk{
				{0, 0x03, 31: PUSH32},
				{31, 0x05},
			},
		},
		// Same as before, but the PUSH32 comes two bytes earlier.
		// This way only 30 bytes of the second chunk should be marked as data.
		"truncated push data at chunk boundary filling partial chunk": {
			code: []byte{0x03, 28: PUSH32, 31: 0x05},
			chunks: []chunk{
				{0, 0x03, 29: PUSH32},
				{30, 0x05},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			chunks := splitCode(test.code)
			require.Equal(test.chunks, chunks)

			merged := merge(chunks, len(test.code))
			require.Equal(test.code, merged)
		})
	}
}
