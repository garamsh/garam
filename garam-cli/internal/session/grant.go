package session

import (
	"context"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/garamsh/garam/lib/errorx"
	platformsdk "github.com/garamsh/garam/lib/platform-sdk-go"
)

// ClientName is what the platform's session list calls this CLI: the
// host it runs on, bounded as the platform bounds it.
func ClientName() string {
	name := "garam CLI"
	if host, err := os.Hostname(); err == nil && host != "" {
		name += " on " + host
	}
	for utf8.RuneCountInString(name) > 100 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

// Start begins a device sign-in (RFC 8628 §3.2) under the CLI's name.
func (s *Session) Start(ctx context.Context) (*platformsdk.DeviceCode, error) {
	client, err := s.Anonymous()
	if err != nil {
		return nil, err
	}
	name := ClientName()
	resp, err := client.RequestDeviceCodeWithFormdataBodyWithResponse(ctx, platformsdk.RequestDeviceCodeFormdataRequestBody{
		ClientId: platformsdk.DeviceCodeRequestClientIdGaramCli, ClientName: &name,
	})
	if err != nil {
		return nil, errorx.Wrap(err, "device code", errorx.KindUnavailable)
	}
	if resp.JSON200 == nil {
		return nil, unexpected(resp.HTTPResponse)
	}
	return resp.JSON200, nil
}

// VerificationURL is the page to approve code on, its path joined to
// the endpoint as the platform answers a path (ADR-0120).
func (s *Session) VerificationURL(code *platformsdk.DeviceCode) string {
	if strings.HasPrefix(code.VerificationUriComplete, "/") {
		return s.Endpoint + code.VerificationUriComplete
	}
	return code.VerificationUriComplete
}

// Wait polls the token endpoint at the interval it sets until code is
// approved, denied or lapses, and keeps the approved session.
func (s *Session) Wait(ctx context.Context, code *platformsdk.DeviceCode) error {
	tokens, err := s.poll(ctx, code)
	if err != nil {
		return err
	}
	now := s.Now()
	return s.Store.Update(func(f *File) (bool, error) {
		e := f.Endpoints[s.Endpoint]
		if e == nil {
			e = &Entry{}
			f.Endpoints[s.Endpoint] = e
		}
		keep(e, tokens, now)
		return true, nil
	})
}

func (s *Session) poll(ctx context.Context, code *platformsdk.DeviceCode) (*platformsdk.SessionToken, error) {
	client, err := s.Anonymous()
	if err != nil {
		return nil, err
	}
	interval := time.Duration(code.Interval) * time.Second
	deadline := s.Now().Add(time.Duration(code.ExpiresIn) * time.Second)
	device := code.DeviceCode
	for {
		select {
		case <-ctx.Done():
			return nil, errorx.Wrap(ctx.Err(), "waiting for the approval", errorx.KindCanceled)
		case <-time.After(interval):
		}
		resp, err := client.DeviceTokenWithFormdataBodyWithResponse(ctx, platformsdk.DeviceTokenFormdataRequestBody{
			ClientId: platformsdk.DeviceTokenRequestClientIdGaramCli, GrantType: platformsdk.UrnIetfParamsOauthGrantTypeDeviceCode,
			DeviceCode: &device,
		})
		switch {
		case err != nil:
			// RFC 8628 §3.5: a client that cannot reach the endpoint
			// keeps polling, backing off, until the code lapses.
			if s.Now().After(deadline) {
				return nil, errorx.Wrap(err, "device token", errorx.KindUnavailable)
			}
			interval += s.SlowDown
			continue
		case resp.JSON200 != nil:
			return resp.JSON200, nil
		case resp.JSON400 == nil:
			return nil, unexpected(resp.HTTPResponse)
		}
		switch resp.JSON400.Error {
		case platformsdk.AuthorizationPending:
		case platformsdk.SlowDown:
			interval += s.SlowDown
			if resp.JSON400.Interval != nil {
				interval = max(interval, time.Duration(*resp.JSON400.Interval)*time.Second)
			}
		case platformsdk.AccessDenied:
			return nil, errorx.New(errorx.KindPermissionDenied, "the sign-in was denied")
		case platformsdk.ExpiredToken:
			return nil, errorx.New(errorx.KindDeadlineExceeded, "the code lapsed before it was approved; run garam login again")
		default:
			return nil, errorx.New(errorx.KindInvalidArgument, "the token endpoint refused the poll: "+string(resp.JSON400.Error))
		}
	}
}
