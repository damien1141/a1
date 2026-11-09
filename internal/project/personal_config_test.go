package project

import (
        "os"
        "path/filepath"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// ---------- positive ----------

func TestPersonalSectionParses_Positive(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
personal:
  enabled: true
  calendar_dir: ~/cal
  mail_dir: /var/mail/me
`), 0o644))
        cfg, err := loadConfig(p.Global())
        require.NoError(t, err)
        assert.True(t, cfg.Personal.EnabledResolved())
        home, err := os.UserHomeDir()
        require.NoError(t, err)
        assert.Equal(t, filepath.Join(home, "cal"), cfg.Personal.CalendarDir, "~ expands at parse")
        assert.Equal(t, "/var/mail/me", cfg.Personal.MailDir)
}

func TestPersonalDefaultsLockedWhenAbsent_Positive(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
`), 0o644))
        cfg, err := loadConfig(p.Global())
        require.NoError(t, err)
        assert.False(t, cfg.Personal.EnabledResolved(), "absent section = locked")
        assert.Equal(t, "", cfg.Personal.CalendarDir)
}

func TestPersonalSectionExplicitOff_Positive(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
personal:
  enabled: false
  calendar_dir: "  ~/cal  "
`), 0o644))
        cfg, err := loadConfig(p.Global())
        require.NoError(t, err)
        assert.False(t, cfg.Personal.EnabledResolved())
        home, _ := os.UserHomeDir()
        assert.Equal(t, filepath.Join(home, "cal"), cfg.Personal.CalendarDir, "paths trim+expand even when disabled")
}

func TestModelPersonalFlagsParse_Positive(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
    personal_read: true
    personal_write: true
  - name: n
    api_key: k2
`), 0o644))
        cfg, err := loadConfig(p.Global())
        require.NoError(t, err)
        m, ok := cfg.FindModel("m")
        require.True(t, ok)
        assert.True(t, m.PersonalRead)
        assert.True(t, m.PersonalWrite)
        n, ok := cfg.FindModel("n")
        require.True(t, ok)
        assert.False(t, n.PersonalRead, "absent flag = locked (the guardrail's default)")
        assert.False(t, n.PersonalWrite)
}

// ---------- negative ----------

func TestPersonalEnabledTypeGarbage_Negative(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
personal:
  enabled: banana
`), 0o644))
        _, err := loadConfig(p.Global())
        require.Error(t, err, "a non-bool enabled is a parse error, never a silent default")
}

// ---------- differential ----------

func TestExpandHomeMatchesNaiveReference_Differential(t *testing.T) {
        home, err := os.UserHomeDir()
        require.NoError(t, err)
        // The reference is spelled out by hand for every boundary shape.
        cases := []struct{ in, want string }{
                {"", ""},
                {"~", home},
                {"~/", home},
                {"~/cal", filepath.Join(home, "cal")},
                {"~/a/b/c.ics", filepath.Join(home, "a", "b", "c.ics")},
                {"/abs/path", "/abs/path"},
                {"rel/path", "rel/path"},
                {"~user/mail", "~user/mail"}, // no ~user lookups, by design
                {"~~/x", "~~/x"},             // only a leading ~ + separator matches
                {"a~", "a~"},
                {" ~spaced", " ~spaced"}, // parse trims first; expandHome itself does not
        }
        for _, tc := range cases {
                assert.Equal(t, tc.want, ExpandHome(tc.in), "ExpandHome(%q)", tc.in)
        }
}

// ---------- mail accounts (chunk 7) ----------

func TestResolvedMailAccounts_Positive(t *testing.T) {
        off := false
        c := PersonalConfig{
                MailDir: "/var/mail/me",
                MailAccounts: []MailAccount{
                        {Name: "work", Type: "imap", Host: "imap.work.dev", Username: "me", Password: "pw", TLS: &off},
                        {Root: "/home/me/Mail"},           // type defaults to maildir, name to the root's basename
                        {Type: "IMAP", Host: "mx.home.dev"}, // type folds case, name defaults to the host
                },
        }
        got := c.ResolvedMailAccounts()
        require.Len(t, got, 4)
        assert.Equal(t, "local", got[0].Name, "the legacy mail_dir prepends as local")
        assert.Equal(t, "maildir", got[0].Type)
        assert.Equal(t, "/var/mail/me", got[0].Root)
        assert.Equal(t, "work", got[1].Name)
        assert.Equal(t, "imap", got[1].Type)
        assert.Equal(t, false, *got[1].TLS, "explicit off survives")
        assert.Equal(t, "Mail", got[2].Name, "unnamed maildir defaults to the root's basename")
        assert.Equal(t, "maildir", got[2].Type, "absent type defaults to maildir")
        assert.Equal(t, "mx.home.dev", got[3].Name, "unnamed imap defaults to the host")
        assert.Equal(t, "imap", got[3].Type, "type folds case")
}

func TestResolvedMailAccountsNamesUnique_Negative(t *testing.T) {
        c := PersonalConfig{
                MailAccounts: []MailAccount{
                        {Name: "same", Root: "/a"},
                        {Name: "same", Root: "/b"},
                        {Name: "same", Root: "/c"},
                },
        }
        got := c.ResolvedMailAccounts()
        require.Len(t, got, 3)
        assert.Equal(t, "same", got[0].Name, "the first keeps the bare name")
        assert.Equal(t, "same-2", got[1].Name, "twins get numeric suffixes")
        assert.Equal(t, "same-3", got[2].Name)
        // The ?account= key must resolve unambiguously after de-duplication.
        names := map[string]bool{}
        for _, a := range got {
                assert.False(t, names[a.Name], "no duplicates remain")
                names[a.Name] = true
        }
}

func TestResolvedMailAccountsEmpty_Negative(t *testing.T) {
        assert.Empty(t, PersonalConfig{}.ResolvedMailAccounts(), "no mail config = no accounts")
        one := PersonalConfig{MailAccounts: []MailAccount{{Name: "x", Root: "/r"}}}
        assert.Len(t, one.ResolvedMailAccounts(), 1, "accounts without mail_dir do not grow a local twin")
}

// TestResolvedMailAccountsMatchesLegacy_Differential: a config that uses
// ONLY the legacy mail_dir resolves to exactly the account the old
// surfaces read — same root, same single-account rail — so flipping a
// config from the old spelling to the new one cannot move the data.
func TestResolvedMailAccountsMatchesLegacy_Differential(t *testing.T) {
        legacy := PersonalConfig{MailDir: "/var/mail/me"}.ResolvedMailAccounts()
        require.Len(t, legacy, 1)
        assert.Equal(t, "/var/mail/me", legacy[0].Root)
        assert.Equal(t, "maildir", legacy[0].Type)

        modern := PersonalConfig{MailAccounts: []MailAccount{{Name: "local", Root: "/var/mail/me"}}}.ResolvedMailAccounts()
        require.Len(t, modern, 1)
        assert.Equal(t, legacy[0].Root, modern[0].Root, "the new spelling reads the same root")
        assert.Equal(t, legacy[0].Type, modern[0].Type)
}

// ---------- mail accounts persistence (chunk 8) ----------

// TestSetMailAccountsRoundTrip_Differential: what SetMailAccounts writes
// must be exactly what parseConfigFile reads back — the write→parse
// round-trip is the configurator's whole contract, and the differential
// is spelled against loadConfig (the real reader), not against
// SetMailAccounts' own inputs reflected in a mirror.
func TestSetMailAccountsRoundTrip_Differential(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`# my a1 config
models:
  - name: m
    api_key: k
personal:
  enabled: true   # the master switch stays
  mail_dir: /var/mail/me
weather:
  enabled: false
`), 0o644))

        off := false
        want := []MailAccount{
                {Name: "work", Type: "imap", Host: "imap.work.dev", Port: 993, Username: "me@work.dev", Password: "apppw keep?:", TLS: &off, Mailbox: "INBOX"},
                {Name: "home", Type: "maildir", Root: "/home/me/Mail"},
        }
        require.NoError(t, SetMailAccounts(p.Global(), want))

        // The file survives the surgical edit with everything this call
        // does not own intact.
        data, err := os.ReadFile(p.Global().ConfigFile())
        require.NoError(t, err)
        text := string(data)
        assert.Contains(t, text, "# my a1 config", "header comment preserved")
        assert.Contains(t, text, "mail_dir: /var/mail/me", "the legacy root is not this call's to move")
        assert.Contains(t, text, "weather:", "unrelated blocks preserved")
        assert.Contains(t, text, "password: 'apppw keep?:'", "the credential round-trips, quoted by the marshaler")

        cfg, err := loadConfig(p.Global())
        require.NoError(t, err)
        got := cfg.Personal.MailAccounts
        require.Len(t, got, 2)
        assert.Equal(t, want[0], got[0], "the imap account round-trips field-for-field")
        assert.Equal(t, want[1], got[1], "the maildir account round-trips field-for-field")
        assert.True(t, cfg.Personal.EnabledResolved(), "the switch survives")
        assert.Equal(t, "/var/mail/me", cfg.Personal.MailDir, "the legacy root survives")
}

func TestSetMailAccountsReplacesWholeList_Positive(t *testing.T) {
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
personal:
  mail_accounts:
    - name: old-one
      root: /old
    - name: old-two
      root: /old2
`), 0o644))

        require.NoError(t, SetMailAccounts(p.Global(), []MailAccount{{Name: "new", Root: "/new"}}))
        cfg, err := loadConfig(p.Global())
        require.NoError(t, err)
        require.Len(t, cfg.Personal.MailAccounts, 1, "the replacement is wholesale, not a merge")
        assert.Equal(t, "new", cfg.Personal.MailAccounts[0].Name)

        // A second save updates in place (no append).
        require.NoError(t, SetMailAccounts(p.Global(), []MailAccount{{Name: "newer", Root: "/newer"}}))
        cfg, err = loadConfig(p.Global())
        require.NoError(t, err)
        require.Len(t, cfg.Personal.MailAccounts, 1)
        assert.Equal(t, "newer", cfg.Personal.MailAccounts[0].Name)
}

func TestSetMailAccountsEmptyListPrunes_Negative(t *testing.T) {
        // An empty save removes the sequence — and prunes the personal
        // block only when nothing else lives there.
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
personal:
  mail_accounts:
    - name: only
      root: /only
`), 0o644))
        require.NoError(t, SetMailAccounts(p.Global(), nil))
        data, err := os.ReadFile(p.Global().ConfigFile())
        require.NoError(t, err)
        text := string(data)
        assert.NotContains(t, text, "mail_accounts", "the sequence is removed")
        assert.NotContains(t, text, "personal:", "an emptied personal block is pruned")
        cfg, err := loadConfig(p.Global())
        require.NoError(t, err)
        assert.Empty(t, cfg.Personal.MailAccounts)
}

func TestSetMailAccountsEmptyListKeepsSiblings_Negative(t *testing.T) {
        // The prune stops at the keys this call does not own: enabled,
        // calendar_dir, and mail_dir survive an empty save untouched.
        p := discoverInTempHome(t)
        require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
personal:
  enabled: true
  calendar_dir: /cal
  mail_dir: /var/mail/me
  mail_accounts:
    - name: only
      root: /only
`), 0o644))
        require.NoError(t, SetMailAccounts(p.Global(), nil))
        data, err := os.ReadFile(p.Global().ConfigFile())
        require.NoError(t, err)
        text := string(data)
        assert.NotContains(t, text, "mail_accounts")
        assert.Contains(t, text, "enabled: true")
        assert.Contains(t, text, "calendar_dir: /cal")
        assert.Contains(t, text, "mail_dir: /var/mail/me")
        cfg, err := loadConfig(p.Global())
        require.NoError(t, err)
        assert.True(t, cfg.Personal.EnabledResolved())
        assert.Empty(t, cfg.Personal.MailAccounts)
}
