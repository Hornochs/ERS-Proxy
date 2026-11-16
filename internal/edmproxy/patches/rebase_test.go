package patches

import "testing"

func TestRebase(t *testing.T) {
	const base = "/d/tok/"
	for _, c := range []struct {
		in, want string
		html     bool
		n        int
	}{
		{`<link href="/assets/a.css" rel="stylesheet"><img src='/logo.png'>`, `<link href="/d/tok/assets/a.css" rel="stylesheet"><img src='/d/tok/logo.png'>`, true, 2},
		{`<a href="/graphs">G</a><form action="/x">`, `<a href="/d/tok/graphs">G</a><form action="/d/tok/x">`, true, 2},
		{`<script src="master.js"></script><a href="https://mt.lv/apps">`, `<script src="master.js"></script><a href="https://mt.lv/apps">`, true, 0},
		{`<a href="//cdn.example/x">`, `<a href="//cdn.example/x">`, true, 0},
		{`<a href="/d/tok/already">`, `<a href="/d/tok/already">`, true, 0},
		{`<a href="/">`, `<a href="/d/tok/">`, true, 1},
		// attributes are left alone in scripts
		{`x='<img src="/a.png">'`, `x='<img src="/a.png">'`, false, 0},
		{"window.location.replace(`/webfig/${window.location.hash}`);", "window.location.replace(`/d/tok/webfig/${window.location.hash}`);", false, 1},
		{"window.location.replace(`/${window.location.hash}`)", "window.location.replace(`/d/tok/${window.location.hash}`)", false, 1},
		{`if (location.pathname.endsWith("/webfig")) location.href = "/webfig/";`, `if (location.pathname.endsWith("/webfig")) location.href = "/d/tok/webfig/";`, true, 1},
		{`location.assign('/x'); location = "/y"`, `location.assign('/d/tok/x'); location = "/d/tok/y"`, false, 2},
		{`location.href = "//other/"`, `location.href = "//other/"`, false, 0},
	} {
		got, n := Rebase(c.in, base, c.html)
		if got != c.want || n != c.n {
			t.Errorf("Rebase(%q)\n got %q (%d)\nwant %q (%d)", c.in, got, n, c.want, c.n)
		}
	}
}
