package shim

import (
	"strings"
	"testing"
)

// TestEscapeCmdArg pins the cmd.exe argument escaping scheme (see
// cmdescape.go). The expected outputs are verified end-to-end against real
// cmd.exe on Windows (TestCmdEscapeEndToEnd) — do not "simplify" them: each
// caret and quote is load-bearing.
func TestEscapeCmdArg(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "plain", `"plain"`},
		{"empty", "", `""`},
		{"spaces only", "two words", `"two words"`},
		{"ampersand", "a&b", `^"a^&b^"`},
		{"pipe", "a|b", `^"a^|b^"`},
		{"caret", "a^b", `^"a^^b^"`},
		{"percent literal", "100%", `^"100^%^"`},
		{"percent var reference", "%PATH%", `^"^%PATH^%^"`},
		{"parens", "(parens)", `^"^(parens^)^"`},
		{"redirect chars", "x<y>z", `^"x^<y^>z^"`},
		{"delayed expansion bangs", "!bang!", `^"^!bang^!^"`},
		{"flag with ampersand", "--flag=val&ue", `^"--flag=val^&ue^"`},
		// Args containing a double quote take the bare-caret form: no
		// surrounding quotes (cmd.exe has no in-quote escape for "), every
		// special/space caret-escaped, so cmd's quote state is never entered.
		{
			"embedded quote",
			`he said "hi"`,
			`he^ said^ ^"hi^"`,
		},
		{
			"injection payload stays one literal token",
			`x" & echo INJECTED & "`,
			`x^"^ ^&^ echo^ INJECTED^ ^&^ ^"`,
		},
		// Trailing backslashes are doubled so the final CommandLineToArgvW
		// parse does not read \" as an escaped quote.
		{"trailing backslash", `C:\out\`, `"C:\out\\"`},
		{"many trailing backslashes", `C:\out\\\`, `"C:\out\\\\\\"`},
		{"interior backslash untouched", `C:\a b\c`, `"C:\a b\c"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := escapeCmdArg(tc.in); got != tc.want {
				t.Errorf("escapeCmdArg(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestEscapeCmdArg_neverUnquotedSpecials asserts the security invariant
// behind both output forms:
//   - Plain-quoted form ("..."): used only when the content has NO cmd
//     specials, so the enclosing quote state alone protects it.
//   - Caret forms (^"..."^ or bare caret): every cmd-special rune is
//     caret-escaped, so cmd.exe's tokenizer can neither split the token nor
//     interpret a metacharacter (& | < > ^ % ...).
func TestEscapeCmdArg_neverUnquotedSpecials(t *testing.T) {
	inputs := []string{
		"&", "|", "^", "<", ">", "(", ")", "%", "!", `"`,
		"a&b|c^d<e>f(g)h%i!j",
		`"&|<>^()%!"`,
		"echo hi & calc.exe",
		"%COMPUTERNAME% & whoami",
		"plain", "two words", "",
	}
	for _, in := range inputs {
		esc := escapeCmdArg(in)
		runes := []rune(esc)
		if len(runes) >= 2 && runes[0] == '"' && runes[len(runes)-1] == '"' {
			// Plain-quoted form: the content must carry no specials at all.
			if strings.ContainsAny(string(runes[1:len(runes)-1]), cmdSpecials) {
				t.Errorf("escapeCmdArg(%q) = %q: plain-quoted form with specials in content", in, esc)
			}
			continue
		}
		// Scan: the caret form must parse as a sequence of plain runes and
		// "^X" escape pairs. Any bare cmd-special (one not introduced by an
		// escape caret) would be interpreted by cmd.exe's tokenizer.
		for i := 0; i < len(runes); i++ {
			if runes[i] == '^' {
				i++ // consume the escaped rune paired with this caret
				continue
			}
			if strings.ContainsRune(cmdSpecials, runes[i]) {
				t.Errorf("escapeCmdArg(%q) = %q: unescaped special %q at index %d", in, esc, runes[i], i)
			}
		}
	}
}

// TestCaretQuoteArg covers the script/program token form: always wrapped in
// caret-escaped quotes, regardless of content (a plain quoted first token
// after /c triggers cmd's quote-stripping heuristic).
func TestCaretQuoteArg(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`D:\x\target.cmd`, `^"D:\x\target.cmd^"`},
		{`C:\Program Files\x\t.cmd`, `^"C:\Program Files\x\t.cmd^"`},
		{`C:\100%\t.cmd`, `^"C:\100^%\t.cmd^"`},
		{`C:\dir\`, `^"C:\dir\\^"`}, // trailing backslash doubled
	}
	for _, tc := range cases {
		if got := caretQuoteArg(tc.in); got != tc.want {
			t.Errorf("caretQuoteArg(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

// TestBuildCmdCommandLine covers the full command-line assembly.
func TestBuildCmdCommandLine(t *testing.T) {
	got := buildCmdCommandLine(`C:\Windows\System32\cmd.exe`, `D:\sdk\npm.cmd`, []string{"install", "a&b"})
	want := `C:\Windows\System32\cmd.exe /c ^"D:\sdk\npm.cmd^" "install" ^"a^&b^"`
	if got != want {
		t.Errorf("buildCmdCommandLine = %q; want %q", got, want)
	}

	// No args: still well-formed.
	got = buildCmdCommandLine(`cmd.exe`, `D:\sdk\run.cmd`, nil)
	want = `cmd.exe /c ^"D:\sdk\run.cmd^"`
	if got != want {
		t.Errorf("buildCmdCommandLine(no args) = %q; want %q", got, want)
	}

	// A cmd.exe path with spaces gets caret-quoted too.
	got = buildCmdCommandLine(`C:\Odd Dir\cmd.exe`, `s.cmd`, []string{"x"})
	want = `^"C:\Odd Dir\cmd.exe^" /c ^"s.cmd^" "x"`
	if got != want {
		t.Errorf("buildCmdCommandLine(spaced cmd.exe) = %q; want %q", got, want)
	}
}

// TestDoubleTrailingBackslashes pins the ArgvQuote helper.
func TestDoubleTrailingBackslashes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\x`, `C:\x`},
		{`C:\x\`, `C:\x\\`},
		{`C:\x\\`, `C:\x\\\\`},
		{``, ``},
		{`\\`, `\\\\`},
		{`no trailing /`, `no trailing /`},
	}
	for _, tc := range cases {
		if got := doubleTrailingBackslashes(tc.in); got != tc.want {
			t.Errorf("doubleTrailingBackslashes(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

// TestIsValidStdHandle pins the console_windows.go guard: GetStdHandle
// signals "no handle" with 0 or INVALID_HANDLE_VALUE (-1). On 64-bit that
// is the all-ones uintptr; a zero-extended 32-bit -1 is rejected too for
// defense in depth. Passing such a value to os.NewFile would create a file
// whose every read/write fails.
func TestIsValidStdHandle(t *testing.T) {
	if isValidStdHandle(0) {
		t.Error("isValidStdHandle(0) = true; want false (NULL handle)")
	}
	if isValidStdHandle(^uintptr(0)) {
		t.Error("isValidStdHandle(^0) = true; want false (INVALID_HANDLE_VALUE)")
	}
	if isValidStdHandle(uintptr(uint32(0xFFFFFFFF))) {
		t.Error("isValidStdHandle(0xFFFFFFFF) = true; want false (32-bit INVALID_HANDLE_VALUE)")
	}
	if !isValidStdHandle(1234) {
		t.Error("isValidStdHandle(1234) = false; want true (normal handle)")
	}
}
