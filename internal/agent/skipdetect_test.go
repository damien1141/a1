package agent

import (
        "testing"
)

// TestFastPathSkip_EmptyAndShort covers the trivial rejection paths.
func TestFastPathSkip_EmptyAndShort(t *testing.T) {
        cases := []struct {
                name string
                msg  string
                want string // expected reason; "" = no skip
        }{
                {"empty", "", "empty"},
                {"whitespace only", "   \t  ", "too_short"},
                {"single word", "hi", "too_short"},
                {"two short words under threshold", "hi there", "too_short"}, // 8 chars
                {"barely meets min", "hello world how are you doing?", ""},    // 30 chars, no skip pattern
        }
        for _, tc := range cases {
                t.Run(tc.name, func(t *testing.T) {
                        got := fastPathSkip(tc.msg)
                        if got != tc.want {
                                t.Errorf("fastPathSkip(%q) = %q, want %q", tc.msg, got, tc.want)
                        }
                })
        }
}

// TestFastPathSkip_URLs covers Pattern 1: many URLs.
func TestFastPathSkip_URLs(t *testing.T) {
        msg := "Check these out: https://example.com https://example.com https://example.com https://example.com https://example.com"
        got := fastPathSkip(msg)
        if got != "many_urls" {
                t.Errorf("fastPathSkip with 5 URLs = %q, want %q", got, "many_urls")
        }
}

// TestFastPathSkip_LongToken covers Pattern 2: long unbroken alphanumeric string.
func TestFastPathSkip_LongToken(t *testing.T) {
        // 100-char hex string — typical of hashes / commit SHAs / base64
        hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef01234567"
        msg := "I committed " + hash + " yesterday."
        got := fastPathSkip(msg)
        if got != "long_token" {
                t.Errorf("fastPathSkip with 100-char hash = %q, want %q", got, "long_token")
        }
}

// TestFastPathSkip_Separators covers Pattern 3: markdown separators.
func TestFastPathSkip_Separators(t *testing.T) {
        cases := []string{"---", "===", "___", "***"}
        for _, sep := range cases {
                t.Run(sep, func(t *testing.T) {
                        msg := "Some intro\n" + sep + "\nmore stuff\n" + sep + "\ntrailer text that goes on"
                        got := fastPathSkip(msg)
                        if got != "separator_chain" {
                                t.Errorf("fastPathSkip with separator %q = %q, want %q", sep, got, "separator_chain")
                        }
                })
        }
}

// TestFastPathSkip_CommandLines covers Pattern 4: command-line patterns.
func TestFastPathSkip_CommandLines(t *testing.T) {
        t.Run("many_commands", func(t *testing.T) {
                msg := "$ git status\n$ git log\n$ git diff\n$ git add .\nworking on the feature"
                got := fastPathSkip(msg)
                if got != "many_commands" {
                        t.Errorf("fastPathSkip with 4 commands = %q, want %q", got, "many_commands")
                }
        })
        t.Run("command_with_url", func(t *testing.T) {
                msg := "$ curl https://example.com/api\nfetched"
                got := fastPathSkip(msg)
                if got != "command_with_url" {
                        t.Errorf("fastPathSkip with command+url = %q, want %q", got, "command_with_url")
                }
        })
}

// TestFastPathSkip_PathDensity covers Pattern 5: high path/URL density.
func TestFastPathSkip_PathDensity(t *testing.T) {
        // Many slashes + dots relative to message length.
        msg := "/usr/local/bin/foo /etc/bar.conf /home/baz/.config/qux.txt /var/log/syslog"
        got := fastPathSkip(msg)
        if got != "path_density" {
                t.Errorf("fastPathSkip with path density = %q, want %q", got, "path_density")
        }
}

// TestFastPathSkip_MarkupDensity covers Pattern 6: structured data.
func TestFastPathSkip_MarkupDensity(t *testing.T) {
        t.Run("markup_ratio", func(t *testing.T) {
                msg := "{{{{}}}}[[[[[]]]]<<<<>>>>" // 20 markup chars out of 20 total
                got := fastPathSkip(msg)
                if got == "" {
                        t.Error("fastPathSkip with all-markup message should skip, got empty reason")
                }
        })
        t.Run("json_density", func(t *testing.T) {
                // Many curly braces = JSON-like.
                msg := "{{{{{{{{{{\"a\":1}}}}}}}}}}" + " padding text to make this long enough to not be too_short"
                got := fastPathSkip(msg)
                if got != "json_density" && got != "markup_density" {
                        t.Errorf("fastPathSkip with curly-heavy JSON = %q, want json_density or markup_density", got)
                }
        })
}

// TestFastPathSkip_PersonalMessage ensures the no-skip path works for
// genuine personal content — the fastPath must let through anything that
// doesn't trip a structural pattern.
func TestFastPathSkip_PersonalMessage(t *testing.T) {
        cases := []string{
                "My name is Solis and I'm working on a Go agent harness.",
                "I just upgraded to Go 1.22 because of the loop variable change.",
                "Yesterday I shipped the new memory injection feature and it feels great.",
                "User prefers terse responses and zero filler in answers.",
        }
        for _, msg := range cases {
                t.Run(msg, func(t *testing.T) {
                        got := fastPathSkip(msg)
                        if got != "" {
                                t.Errorf("fastPathSkip on personal message %q returned %q, want empty (allow)", msg, got)
                        }
                })
        }
}

// TestFastPathSkip_CodeBlock covers Pattern 9: code-like indentation.
func TestFastPathSkip_CodeBlock(t *testing.T) {
        // Multi-line code with indented blocks ending in {, }, ;, etc.
        msg := `Here is the function:
  func main() {
    if x {
      doThing();
    }
  }
done.`
        got := fastPathSkip(msg)
        if got == "" {
                t.Error("fastPathSkip on code block should skip, got empty")
        }
}

// TestFastPathSkip_SpecialCharDensity covers Pattern 10: high special chars.
func TestFastPathSkip_SpecialCharDensity(t *testing.T) {
        // Many non-alphanumeric, non-space chars (encoded data, base64-ish).
        msg := "@#$%^&*()[]{}<>?/|\\~`+=!@#$%^&*()[]{}<>?/|\\~`+=!@#$%^&*()[]{}<>?/|\\~`+=!@#$%^&*()[]{}<>?/|\\"
        got := fastPathSkip(msg)
        if got == "" {
                t.Error("fastPathSkip on high-special-char message should skip, got empty")
        }
}
