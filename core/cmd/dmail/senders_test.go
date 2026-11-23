package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/arqueon/dux-mail/core/models"
)

func TestFormatListSenders(t *testing.T) {
	acct := "ruben.garcia.sanchez@udg.mx"
	meFull := "RUBEN JUAN SEBASTIAN GARCIA SANCHEZ <ruben.garcia.sanchez@udg.mx>"
	martin := "MARTIN PIZANO MORENO <martin.pizano@udg.mx>"
	melanie := "MELANIE REBECA ESTRADA SANCHEZ <melanie@alumnos.udg.mx>"

	cases := []struct {
		name string
		t    models.ThreadSummary
		want string
	}{
		{
			name: "single message",
			t: models.ThreadSummary{
				Participants: []string{"Notificaciones <n@banamex.com>"},
				LastSender:   "Notificaciones <n@banamex.com>",
				MessageCount: 1,
			},
			want: "Notificaciones",
		},
		{
			name: "self then other with count",
			t: models.ThreadSummary{
				Participants: []string{meFull, martin},
				LastSender:   martin,
				MessageCount: 2,
			},
			want: "yo, MARTIN PIZANO MORENO (2)",
		},
		{
			name: "round trip A -> self -> A promotes LastSender and preserves count",
			t: models.ThreadSummary{
				Participants: []string{melanie, meFull},
				LastSender:   melanie,
				MessageCount: 3,
			},
			want: "yo, MELANIE REBECA ESTR… (3)",
		},
		{
			name: "many participants collapsed with ellipsis and suffix preserved",
			t: models.ThreadSummary{
				Participants: []string{
					"Ernesto Antonio Zarco Ortiz <ezarco@colmex.mx>",
					"Lucas Ruppel <lruppel@ucdavis.edu>",
					"Macarena Francisca Williamson Modesto <maca@gmail.com>",
				},
				LastSender:   "Macarena Francisca Williamson Modesto <maca@gmail.com>",
				MessageCount: 11,
			},
			want: "Ernesto, …, Macarena F… (11)",
		},
		{
			name: "same sender repeated shows count",
			t: models.ThreadSummary{
				Participants: []string{"Notificaciones <n@banamex.com>"},
				LastSender:   "Notificaciones <n@banamex.com>",
				MessageCount: 3,
			},
			want: "Notificaciones (3)",
		},
		{
			name: "self only thread",
			t: models.ThreadSummary{
				Participants: []string{meFull},
				LastSender:   meFull,
				MessageCount: 3,
			},
			want: "yo (3)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatListSenders(tc.t, acct, "yo", 28)
			if utf8.RuneCountInString(got) != 28 {
				t.Fatalf("rune width = %d, want 28 (%q)", utf8.RuneCountInString(got), got)
			}
			if strings.TrimRight(got, " ") != tc.want {
				t.Fatalf("got %q, want %q", strings.TrimRight(got, " "), tc.want)
			}
		})
	}
}
