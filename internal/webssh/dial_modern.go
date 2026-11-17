package webssh

// dial_legacy.go is generated from this file (see TestLegacyInSync): keep the
// two identical apart from the import and the function names.

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// dialModern connects with one SSH implementation; insecure adds that
// implementation's insecure algorithms to its defaults.
func dialModern(ctx context.Context, o *Options, insecure bool) (*Session, error) {
	algs := ssh.SupportedAlgorithms()
	if insecure {
		in := ssh.InsecureAlgorithms()
		algs.KeyExchanges = appendNew(algs.KeyExchanges, in.KeyExchanges)
		algs.Ciphers = appendNew(algs.Ciphers, in.Ciphers)
		algs.MACs = appendNew(algs.MACs, in.MACs)
		algs.HostKeys = appendNew(algs.HostKeys, in.HostKeys)
	}
	var info Info
	var banner bannerBuffer
	cfg := &ssh.ClientConfig{
		User: o.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(o.Password),
			ssh.KeyboardInteractive(func(_, _ string, questions []string, echos []bool) ([]string, error) {
				return answers(o, questions, echos), nil
			}),
		},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			info.Fingerprint = ssh.FingerprintSHA256(key)
			return checkHostKey(key.Type(), info.Fingerprint, o.Pin)
		},
		HostKeyAlgorithms: algs.HostKeys,
		BannerCallback:    banner.add,
		Timeout:           o.Timeout,
		Config:            ssh.Config{KeyExchanges: algs.KeyExchanges, Ciphers: algs.Ciphers, MACs: algs.MACs},
	}
	conn, err := dial(ctx, o)
	if err != nil {
		return nil, err
	}
	// Until the shell runs, a cancelled context or the timeout ends the attempt.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(o.Timeout))

	c, chans, reqs, err := ssh.NewClientConn(conn, o.Addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, mapErrModern(err)
	}
	client := ssh.NewClient(c, chans, reqs)
	if a, ok := c.(ssh.AlgorithmsConnMetadata); ok {
		na := a.Algorithms()
		info.KeyExchange, info.HostKey, info.Cipher, info.MAC = na.KeyExchange, na.HostKey, na.Write.Cipher, na.Write.MAC
	}
	info.Banner = banner.b.String()

	sess, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}
	if err := sess.RequestPty("xterm", o.Rows, o.Cols, modes); err != nil {
		_ = client.Close()
		return nil, err
	}
	in, err := sess.StdinPipe()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	pr, pw := io.Pipe()
	sess.Stdout, sess.Stderr = pw, pw
	if err := sess.Shell(); err != nil {
		_ = client.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})

	s := newSession(in, pr, info,
		func(cols, rows int) error { return sess.WindowChange(rows, cols) },
		client.Close)
	go func() {
		err := sess.Wait()
		var exit *ssh.ExitError
		var missing *ssh.ExitMissingError
		if errors.As(err, &exit) || errors.As(err, &missing) {
			err = nil // the shell ended; how is not an error of the session
		}
		_ = client.Close()
		_ = pw.Close()
		s.finish(err)
	}()
	return s, nil
}

// mapErrModern turns errors of the SSH implementation into this package's.
func mapErrModern(err error) error {
	var hk *HostKeyError
	if errors.As(err, &hk) {
		return hk
	}
	var ane *ssh.AlgorithmNegotiationError
	if errors.As(err, &ane) {
		return &AlgorithmError{What: ane.What, Offered: ane.RequestedAlgorithms, Supported: ane.SupportedAlgorithms}
	}
	if isAuthError(err) {
		return ErrAuth
	}
	return err
}
