package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type secretCfg struct{ User, Password string }

func (c secretCfg) LogValue() slog.Value {
	return slog.GroupValue(slog.String("user", c.User), slog.String("password", c.Password))
}

// logSecrets writes one record per leak path through l.
func logSecrets(l *slog.Logger) {
	l.Info("attrs",
		"password", "sec-1", "DB_PASSWORD", "sec-2", "X-Api-Key", "sec-3",
		"Authorization", "Bearer sec-4", "set-cookie", "sec-5", "clientSecret", "sec-6",
		"custom_pin", "sec-7", "user", "alice", "empty_token", "")
	l.Info("nested", slog.Group("req", slog.Group("headers", "cookie", "sec-8")),
		slog.Group("credentials", "user", "sec-9"), "cfg", secretCfg{User: "bob", Password: "sec-10"})
	l.With("refresh_token", "sec-11").WithGroup("oauth").Info("with", "access_token", "sec-12")
	l.WithGroup("secret").Info("group", "anything", "sec-13")
}

func assertRedacted(t *testing.T, out string) {
	t.Helper()
	assert.NotContains(t, out, "sec-", "a secret reached the output")
	assert.Contains(t, out, RedactedValue)
	assert.Contains(t, out, "alice", "non-sensitive values must stay")
	assert.Contains(t, out, "bob")
}

func TestConsole_RedactsSensitiveKeys(t *testing.T) {
	for _, env := range []string{EnvDevelopment, EnvProduction} {
		t.Run(env, func(t *testing.T) {
			resetSingleton(t)
			out := captureStdout(t, func() {
				require.NoError(t, InitGlobal(context.Background(), Config{
					ServiceName: "svc", Environment: env, RedactKeys: []string{"Custom_PIN"},
				}))
				logSecrets(Instance().Logger)
				logSecrets(slog.Default())
			})
			assertRedacted(t, out)
		})
	}
}

func TestAttach_RedactsRecordsForExporters(t *testing.T) {
	resetSingleton(t)
	var exported bytes.Buffer
	captureStdout(t, func() {
		require.NoError(t, InitGlobal(context.Background(), Config{
			ServiceName: "svc", Environment: EnvProduction, RedactKeys: []string{"custom_pin"},
		}))
		// A plain handler without ReplaceAttr stands in for the OTLP bridge.
		require.NoError(t, Attach(slog.NewJSONHandler(&exported, nil), nil))
		logSecrets(Instance().Logger)
	})
	assertRedacted(t, exported.String())
	assert.Equal(t, 4, strings.Count(exported.String(), "\n"))
}

func TestNewRedactHandlerAndRedactAttr(t *testing.T) {
	var a, b bytes.Buffer
	logSecrets(slog.New(NewRedactHandler(slog.NewJSONHandler(&a, nil), "custom_pin")))
	logSecrets(slog.New(slog.NewTextHandler(&b, &slog.HandlerOptions{ReplaceAttr: RedactAttr("custom_pin")})))
	assertRedacted(t, a.String())
	assertRedacted(t, b.String())
}

func TestRedactor_Matching(t *testing.T) {
	r := newRedactor([]string{" Tax-ID "})
	for _, k := range []string{"password", "PASSWORD", "db_password", "dbPassword", "x-api-key", "Set-Cookie", "private_key", "tax_id", "credentials"} {
		assert.True(t, r.sensitive(k), k)
	}
	for _, k := range []string{"user", "msg", "level", "token_ttl", "tokens", "service", "passwords_checked"} {
		assert.False(t, r.sensitive(k), k)
	}
	assert.NotEmpty(t, DefaultRedactKeys())
}
