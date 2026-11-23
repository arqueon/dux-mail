package main

import (
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/arqueon/dux-mail/core/models"
)

var rfc2047Decoder = new(mime.WordDecoder)

func decodeHeaderWords(s string) string {
	if !strings.Contains(s, "=?") {
		return s
	}
	if dec, err := rfc2047Decoder.DecodeHeader(s); err == nil {
		return dec
	}
	return s
}

func parseSenderAddress(raw string) (name, email string) {
	s := strings.TrimSpace(decodeHeaderWords(raw))
	if s == "" {
		return "", ""
	}
	if addr, err := mail.ParseAddress(s); err == nil {
		return strings.Trim(strings.TrimSpace(addr.Name), `"`), strings.TrimSpace(addr.Address)
	}
	if i := strings.LastIndex(s, "<"); i >= 0 {
		if j := strings.LastIndex(s, ">"); j > i {
			namePart := strings.Trim(strings.TrimSpace(s[:i]), `"`)
			emailPart := strings.TrimSpace(s[i+1 : j])
			return namePart, emailPart
		}
	}
	return "", strings.Trim(s, "<> ")
}

func senderDisplayName(raw string) string {
	name, email := parseSenderAddress(raw)
	if name != "" {
		return name
	}
	return email
}

func isSelfSender(raw, accountEmail string) bool {
	acct := strings.TrimSpace(accountEmail)
	if acct == "" {
		return false
	}
	_, email := parseSenderAddress(raw)
	return strings.EqualFold(email, acct)
}

func truncateRunes(s string, maxLen int) string {
	if maxLen <= 0 || utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if maxLen == 1 {
		return "…"
	}
	return string(runes[:maxLen-1]) + "…"
}

func shortSenderName(raw, accountEmail, meLabel string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 14
	}
	if meLabel == "" {
		meLabel = "me"
	}
	if isSelfSender(raw, accountEmail) {
		return meLabel
	}
	name := senderDisplayName(raw)
	if name == "" {
		return ""
	}
	var res string
	if !strings.ContainsAny(name, " \t\n\r") && strings.Contains(name, "@") {
		res = strings.SplitN(name, "@", 2)[0]
	} else {
		fields := strings.Fields(name)
		tokens := make([]string, 0, len(fields))
		for _, tok := range fields {
			cleaned := strings.Trim(tok, `,;"'`)
			if cleaned != "" {
				tokens = append(tokens, cleaned)
			}
		}
		switch {
		case len(tokens) == 0:
			res = name
		case len(tokens) > 1 && (strings.HasSuffix(tokens[0], ".") || utf8.RuneCountInString(tokens[0]) <= 2):
			res = tokens[0] + " " + tokens[1]
		default:
			res = tokens[0]
		}
	}
	return truncateRunes(res, maxLen)
}

func senderParticipantKey(raw, accountEmail string) string {
	if isSelfSender(raw, accountEmail) {
		return "__self__"
	}
	name, email := parseSenderAddress(raw)
	if name != "" {
		return strings.ToLower(name)
	}
	return strings.ToLower(email)
}

func formatListSenders(t models.ThreadSummary, accountEmail, meLabel string, maxWidth int) string {
	if meLabel == "" {
		meLabel = "me"
	}
	ordered := make([]string, 0, len(t.Participants)+1)
	seen := make(map[string]bool, len(t.Participants)+1)
	for _, p := range t.Participants {
		if strings.TrimSpace(p) == "" {
			continue
		}
		k := senderParticipantKey(p, accountEmail)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		ordered = append(ordered, p)
	}

	latestRaw := strings.TrimSpace(t.LastSender)
	if latestRaw == "" && len(ordered) > 0 {
		latestRaw = ordered[len(ordered)-1]
	}
	if latestRaw != "" {
		latestKey := senderParticipantKey(latestRaw, accountEmail)
		filtered := ordered[:0]
		for _, p := range ordered {
			if senderParticipantKey(p, accountEmail) != latestKey {
				filtered = append(filtered, p)
			}
		}
		ordered = append(filtered, latestRaw)
	}

	var namePart string
	if len(ordered) > 0 {
		lastEntry := ordered[len(ordered)-1]
		latestLabel := senderDisplayName(lastEntry)
		if isSelfSender(lastEntry, accountEmail) {
			latestLabel = meLabel
		}
		switch len(ordered) {
		case 1:
			namePart = latestLabel
		case 2:
			namePart = shortSenderName(ordered[0], accountEmail, meLabel, 14) + ", " + latestLabel
		default:
			namePart = shortSenderName(ordered[0], accountEmail, meLabel, 14) + ", …, " + latestLabel
		}
	}

	suffix := ""
	if t.MessageCount > 1 {
		suffix = fmt.Sprintf(" (%d)", t.MessageCount)
	}

	out := namePart + suffix
	if maxWidth > 0 {
		totalRunes := utf8.RuneCountInString(out)
		if totalRunes > maxWidth {
			suffixRunes := utf8.RuneCountInString(suffix)
			avail := maxWidth - suffixRunes
			if avail <= 1 {
				out = truncateRunes(out, maxWidth)
			} else {
				out = truncateRunes(namePart, avail) + suffix
			}
		}
		if pad := maxWidth - utf8.RuneCountInString(out); pad > 0 {
			out += strings.Repeat(" ", pad)
		}
	}
	return out
}
