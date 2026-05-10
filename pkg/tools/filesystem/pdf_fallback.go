package filesystem

import (
	"bytes"
	"compress/zlib"
	"io"
	"regexp"
	"strings"
)

var (
	// Matches the body of any "stream ... endstream" block in a PDF.
	pdfStreamRe = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)
	// Matches a (Foo Bar) literal-string PDF text operand, optionally followed by
	// the Tj or TJ text-show operator. We capture the literal payload.
	pdfTextLiteralRe = regexp.MustCompile(`\(((?:\\.|[^()\\])*)\)\s*T[jJ]`)
)

// decodePDFTextFallback extracts a best-effort text representation from a raw
// PDF byte stream. It is NOT a complete PDF parser: it locates content streams,
// optionally decompresses FlateDecode-style payloads, and pulls out PDF
// literal-string text operands. This is sufficient to summarise typical
// modern PDFs without pulling a heavy dependency, and degrades gracefully
// (returning whatever it could find) when the document is encrypted or uses
// only CMap-encoded glyphs.
func decodePDFTextFallback(data []byte) string {
	var out strings.Builder
	matches := pdfStreamRe.FindAllSubmatchIndex(data, -1)
	for _, m := range matches {
		stream := data[m[2]:m[3]]
		decoded := tryFlateDecode(stream)
		if decoded == nil {
			decoded = stream
		}
		extractTextOperands(decoded, &out)
	}
	if out.Len() == 0 {
		// Last-ditch: scan the raw file for text operands.
		extractTextOperands(data, &out)
	}
	return strings.TrimSpace(out.String())
}

func tryFlateDecode(in []byte) []byte {
	r, err := zlib.NewReader(bytes.NewReader(in))
	if err != nil {
		return nil
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return nil
	}
	return out
}

func extractTextOperands(in []byte, out *strings.Builder) {
	for _, m := range pdfTextLiteralRe.FindAllSubmatch(in, -1) {
		payload := string(m[1])
		out.WriteString(unescapePDFLiteral(payload))
		out.WriteString(" ")
	}
}

// unescapePDFLiteral handles the PDF literal-string escape sequences used in
// content streams. Non-printable escapes are translated to their ASCII forms;
// unknown escapes pass through unchanged.
func unescapePDFLiteral(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(s) {
			break
		}
		next := s[i+1]
		switch next {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case '(', ')', '\\':
			b.WriteByte(next)
		default:
			b.WriteByte(next)
		}
		i += 2
	}
	return b.String()
}
