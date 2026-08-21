package shim

import (
	"strings"
)

// cmdSpecials are the metacharacters cmd.exe interprets outside of quoted
// regions. An unescaped occurrence in a command line can truncate arguments
// (& | < >), chain commands (& |), escape (^), expand variables (%), toggle
// quote state ("), group ( ), or trigger delayed expansion (!).
const cmdSpecials = `()%!^"<>&|`

// escapeCmdArg escapes a single argument for inclusion in a `cmd.exe /c`
// command line so the target batch script receives it verbatim.
//
// Go's default CreateProcess quoting (CommandLineToArgvW-style) is not
// understood by cmd.exe: with exec.Command("cmd.exe", "/c", script, args...)
// an argument like `a&b` is re-tokenized by cmd.exe (& becomes a command
// separator -> argument truncation + command injection). This function
// applies a two-layer scheme verified against real cmd.exe behaviour:
//
//   - Args containing a double quote are emitted UNQUOTED with every
//     cmd-special character, space and tab caret-escaped (bareCaretArg).
//     cmd.exe has no escape sequence for " inside a quoted region (it does
//     NOT understand \"), so any quoted form would corrupt cmd's quote-state
//     tracking for the rest of the line. Caret-escaping never enters quote
//     state, which is what makes adversarial arguments injection-safe.
//     (An embedded quote cannot be transported losslessly through a batch
//     file's %* re-parse — that limitation is inherent to cmd.exe and
//     identical to invoking the batch script directly.)
//   - Other args are wrapped in double quotes (quotes protect spaces and
//     specials from cmd's tokenizer). If the arg contains cmd specials the
//     enclosing quotes are additionally caret-escaped: a leading plain
//     quoted token right after /c triggers cmd's quote-stripping heuristic
//     and can swallow subsequent tokens, and caret-escaping the % prevents
//     cmd from expanding %VAR% references inside the argument.
//   - Runs of trailing backslashes are doubled before the closing quote so
//     the final program's CommandLineToArgvW parse does not turn \" into a
//     literal quote (standard ArgvQuote rule).
func escapeCmdArg(s string) string {
	if strings.Contains(s, `"`) {
		return bareCaretArg(s)
	}
	quoted := `"` + doubleTrailingBackslashes(s) + `"`
	if !strings.ContainsAny(s, cmdSpecials) {
		return quoted
	}
	return caretEscape(quoted)
}

// caretQuoteArg wraps s in caret-escaped double quotes unconditionally.
// Used for the program/script token right after `cmd.exe /c`: a plain
// quoted first token makes cmd.exe's /c quote-stripping heuristic swallow
// following quoted arguments into the program name.
func caretQuoteArg(s string) string {
	return caretEscape(`"` + doubleTrailingBackslashes(s) + `"`)
}

// caretEscape prefixes every cmd-special character in s with ^.
func caretEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, r := range s {
		if strings.ContainsRune(cmdSpecials, r) {
			b.WriteByte('^')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// bareCaretArg emits s with no surrounding quotes, caret-escaping every
// cmd-special character plus space and tab so cmd.exe's tokenizer keeps the
// argument as a single literal token without ever entering quote state.
func bareCaretArg(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, r := range s {
		if strings.ContainsRune(cmdSpecials, r) || r == ' ' || r == '\t' {
			b.WriteByte('^')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// doubleTrailingBackslashes doubles any run of backslashes at the end of s.
// CommandLineToArgvW treats 2n+1 backslashes before a closing " as an
// escaped quote, so "C:\dir\" would not terminate the quoted region;
// "C:\dir\\" parses correctly to C:\dir\.
func doubleTrailingBackslashes(s string) string {
	n := 0
	for n < len(s) && s[len(s)-1-n] == '\\' {
		n++
	}
	if n == 0 {
		return s
	}
	return s + strings.Repeat(`\`, n)
}

// buildCmdCommandLine assembles the full CreateProcess command line for
// running a .cmd/.bat script through cmd.exe /c with args escaped via
// escapeCmdArg. cmdExe is the resolved path of cmd.exe; it is emitted bare
// when possible and caret-quoted otherwise (paths containing spaces).
func buildCmdCommandLine(cmdExe, script string, args []string) string {
	prog := cmdExe
	if strings.ContainsAny(prog, cmdSpecials+" \t") {
		prog = caretQuoteArg(prog)
	}
	parts := make([]string, 0, len(args)+3)
	parts = append(parts, prog, "/c", caretQuoteArg(script))
	for _, a := range args {
		parts = append(parts, escapeCmdArg(a))
	}
	return strings.Join(parts, " ")
}
