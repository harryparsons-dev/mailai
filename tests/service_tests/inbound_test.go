package servicetests

import (
	"strings"
	"testing"

	"mailai/services/inbound"
)

func TestGenerateAddress(t *testing.T) {
	cases := []struct {
		name    string
		project string
		attempt int
		want    string
	}{
		{"short name", "Inbox", 1, "inbox@mail.example.com"},
		{"words joined with hyphens", "My App", 1, "my-app@mail.example.com"},
		{"cut to ten characters", "Daily summary", 1, "daily-summ@mail.example.com"},
		{"no trailing hyphen after cut", "Marketing team", 1, "marketing@mail.example.com"},
		{"symbols and spaces collapse", "  --Q4 // Sales!! ", 1, "q4-sales@mail.example.com"},
		{"non-ASCII letters dropped", "Café Ünits", 1, "caf-nits@mail.example.com"},
		{"nothing usable falls back", "日本語 !!!", 1, "project@mail.example.com"},
		{"second attempt adds suffix", "Inbox", 2, "inbox-2@mail.example.com"},
		{"suffix replaces end of long slug", "Daily summary", 2, "daily-su-2@mail.example.com"},
		{"suffix trims hyphen left by cut", "Daily summary", 10, "daily-s-10@mail.example.com"},
		{"suffix on hyphen boundary", "abcdefg hij", 2, "abcdefg-2@mail.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inbound.GenerateAddress("Mail.Example.com", tc.project, tc.attempt)
			if got != tc.want {
				t.Fatalf("GenerateAddress(%q, %d) = %q, want %q", tc.project, tc.attempt, got, tc.want)
			}
			local, _, _ := strings.Cut(got, "@")
			if len(local) > 10 {
				t.Fatalf("local part %q is longer than 10 characters", local)
			}
		})
	}
}

func TestNormalizeAddress(t *testing.T) {
	if got := inbound.NormalizeAddress(" <Abc@Example.COM> "); got != "abc@example.com" {
		t.Fatalf("got %q", got)
	}
	if got := inbound.AddressDomain("a@b@example.com"); got != "example.com" {
		t.Fatalf("got %q", got)
	}
}

func crlf(s string) []byte {
	return []byte(strings.ReplaceAll(s, "\n", "\r\n"))
}

func TestParseMultipart(t *testing.T) {
	raw := crlf(`From: "Jane Doe" <Jane@Example.com>
To: abc@mail.test, other@example.com
Subject: =?UTF-8?Q?Caf=C3=A9_update?=
Date: Mon, 02 Nov 2026 10:00:00 +0000
Message-ID: <msg-1@example.com>
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="outer"

--outer
Content-Type: multipart/alternative; boundary="inner"

--inner
Content-Type: text/plain; charset=iso-8859-1
Content-Transfer-Encoding: quoted-printable

Hello caf=E9
--inner
Content-Type: text/html; charset=utf-8

<p>Hello</p>
--inner--
--outer
Content-Type: application/pdf
Content-Disposition: attachment; filename="report.pdf"
Content-Transfer-Encoding: base64

aGVsbG8=
--outer--
`)

	parsed, err := inbound.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.FromAddress != "jane@example.com" || parsed.FromName != "Jane Doe" {
		t.Errorf("from = %q %q", parsed.FromAddress, parsed.FromName)
	}
	if parsed.ToAddresses != "abc@mail.test, other@example.com" {
		t.Errorf("to = %q", parsed.ToAddresses)
	}
	if parsed.Subject != "Café update" {
		t.Errorf("subject = %q", parsed.Subject)
	}
	if parsed.MessageID != "msg-1@example.com" {
		t.Errorf("message id = %q", parsed.MessageID)
	}
	if parsed.SentAt == nil || parsed.SentAt.Year() != 2026 {
		t.Errorf("sent at = %v", parsed.SentAt)
	}
	if strings.TrimSpace(parsed.TextBody) != "Hello café" {
		t.Errorf("text body = %q", parsed.TextBody)
	}
	if strings.TrimSpace(parsed.HTMLBody) != "<p>Hello</p>" {
		t.Errorf("html body = %q", parsed.HTMLBody)
	}
	if len(parsed.Attachments) != 1 {
		t.Fatalf("attachments = %+v", parsed.Attachments)
	}
	if a := parsed.Attachments[0]; a.Filename != "report.pdf" || a.ContentType != "application/pdf" || a.Size != 5 {
		t.Errorf("attachment = %+v", a)
	}
}

func TestParsePlainMessage(t *testing.T) {
	parsed, err := inbound.Parse(crlf("From: a@example.com\nSubject: hi\n\nplain body\x00\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(parsed.TextBody) != "plain body" {
		t.Errorf("text body = %q", parsed.TextBody)
	}
	if parsed.Attachments == nil {
		t.Error("attachments should be an empty slice, not nil")
	}
}

func TestParseGarbageKeepsGoing(t *testing.T) {
	parsed, _ := inbound.Parse([]byte("not an email at all"))
	if parsed == nil {
		t.Fatal("expected a non-nil result")
	}
}
