package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/garamsh/garam/lib/errorx"
	apigen "github.com/garamsh/garam/lib/platform-sdk-go"
)

// newLoginCommand signs the CLI in by the device grant (ADR-0120).
func newLoginCommand() *cobra.Command {
	flags := &userFlags{}
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign the CLI in, approving it in a browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newUserCommand(cmd, flags)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			return c.login(ctx, cmd, !noBrowser)
		},
	}
	flags.bind(cmd)
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the URL to open rather than opening it")
	return cmd
}

func (c *userCommand) login(ctx context.Context, cmd *cobra.Command, browse bool) error {
	client, err := c.anonymous()
	if err != nil {
		return err
	}
	name := clientName()
	issued, err := client.RequestDeviceCodeWithFormdataBodyWithResponse(ctx, apigen.RequestDeviceCodeFormdataRequestBody{
		ClientId: apigen.DeviceCodeRequestClientIdGaramCli, ClientName: &name,
	})
	if err != nil {
		return errorx.Wrap(err, "device code", errorx.KindUnavailable)
	}
	if issued.JSON200 == nil {
		return apiFailure(issued.HTTPResponse, issued.Body)
	}
	grant := issued.JSON200
	page := c.verificationURL(grant.VerificationUriComplete)
	prompt := cmd.ErrOrStderr()
	_, _ = fmt.Fprintf(prompt, "Your code is %s. Approve it at\n\n  %s\n\n", grant.UserCode, page)
	if browse && openBrowser(page) == nil {
		_, _ = fmt.Fprintln(prompt, "Opened that page in your browser; waiting for the approval.")
	} else {
		_, _ = fmt.Fprintln(prompt, "Open that page in a browser; waiting for the approval.")
	}
	tokens, err := c.poll(ctx, client, grant)
	if err != nil {
		return err
	}
	now := c.now()
	if err := c.store.update(func(file *credentialsFile) (bool, error) {
		entry := file.Endpoints[c.endpoint]
		if entry == nil {
			entry = &endpointCredentials{}
			file.Endpoints[c.endpoint] = entry
		}
		entry.AccessToken = tokens.AccessToken
		entry.AccessExpiresAt = now.Add(time.Duration(tokens.ExpiresIn) * time.Second)
		entry.RefreshToken = tokens.RefreshToken
		entry.RefreshExpiresAt = now.Add(time.Duration(tokens.RefreshTokenExpiresIn) * time.Second)
		return true, nil
	}); err != nil {
		return err
	}
	who, err := c.me(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(prompt, "Signed in to %s as %s (%s).\n", c.endpoint, who.Nickname, who.Grn)
	return nil
}

// poll asks the token endpoint at the interval it sets until the code
// is decided or lapses.
func (c *userCommand) poll(ctx context.Context, client *apigen.ClientWithResponses, grant *apigen.DeviceCode) (*apigen.SessionToken, error) {
	interval := time.Duration(grant.Interval) * time.Second
	deadline := time.Now().Add(time.Duration(grant.ExpiresIn) * time.Second)
	code := grant.DeviceCode
	for {
		select {
		case <-ctx.Done():
			return nil, errorx.Wrap(ctx.Err(), "waiting for the approval", errorx.KindCanceled)
		case <-time.After(interval):
		}
		resp, err := client.DeviceTokenWithFormdataBodyWithResponse(ctx, apigen.DeviceTokenFormdataRequestBody{
			ClientId: apigen.DeviceTokenRequestClientIdGaramCli, GrantType: apigen.UrnIetfParamsOauthGrantTypeDeviceCode,
			DeviceCode: &code,
		})
		switch {
		case err != nil:
			// RFC 8628 §3.5: a client that cannot reach the endpoint
			// keeps polling, backing off, until the code lapses.
			if time.Now().After(deadline) {
				return nil, errorx.Wrap(err, "device token", errorx.KindUnavailable)
			}
			interval += c.slowDown
			continue
		case resp.JSON200 != nil:
			return resp.JSON200, nil
		case resp.JSON400 == nil:
			return nil, apiFailure(resp.HTTPResponse, resp.Body)
		}
		switch resp.JSON400.Error {
		case apigen.AuthorizationPending:
		case apigen.SlowDown:
			interval += c.slowDown
			if resp.JSON400.Interval != nil {
				interval = max(interval, time.Duration(*resp.JSON400.Interval)*time.Second)
			}
		case apigen.AccessDenied:
			return nil, errorx.New(errorx.KindPermissionDenied, "the sign-in was denied")
		case apigen.ExpiredToken:
			return nil, errorx.New(errorx.KindDeadlineExceeded, "the code lapsed before it was approved; run garam login again")
		default:
			return nil, errorx.New(errorx.KindInvalidArgument, "the token endpoint refused the poll: "+string(resp.JSON400.Error))
		}
	}
}

// verificationURL joins the page the API answered, a path (ADR-0120),
// to the endpoint the command talks to.
func (c *userCommand) verificationURL(page string) string {
	if strings.HasPrefix(page, "/") {
		return c.endpoint + page
	}
	return page
}

// clientName is what the session list calls this CLI: the host it
// runs on, bounded as the API bounds it.
func clientName() string {
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

// openBrowser asks the desktop to open page, and reports whether it
// could ask; whether a browser came up is not something it can see.
func openBrowser(page string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	cmd := exec.Command(opener, page)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// newLogoutCommand revokes the CLI's session and forgets its tokens.
func newLogoutCommand() *cobra.Command {
	flags := &userFlags{}
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Revoke the CLI's session and forget its tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newUserCommand(cmd, flags)
			if err != nil {
				return err
			}
			return c.logout(cmd)
		},
	}
	flags.bind(cmd)
	return cmd
}

func (c *userCommand) logout(cmd *cobra.Command) error {
	client, err := c.signedIn(cmd.Context())
	if errorx.IsKind(err, errorx.KindUnauthenticated) {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Not signed in to %s.\n", c.endpoint)
		return c.forget()
	}
	if err != nil {
		return err
	}
	resp, err := client.LogoutWithResponse(cmd.Context())
	if err != nil {
		return errorx.Wrap(err, "logout", errorx.KindUnavailable)
	}
	// 401 is a session already revoked elsewhere: what logout asks for
	// is already so.
	if code := resp.StatusCode(); code != 204 && code != 401 {
		return apiFailure(resp.HTTPResponse, resp.Body)
	}
	if err := c.forget(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Signed out of %s.\n", c.endpoint)
	return nil
}

// forget drops this endpoint's tokens, keeping its saved organization.
func (c *userCommand) forget() error {
	return c.store.update(func(file *credentialsFile) (bool, error) {
		entry := file.Endpoints[c.endpoint]
		if entry == nil {
			return false, nil
		}
		if entry.Org == "" {
			delete(file.Endpoints, c.endpoint)
		} else {
			file.Endpoints[c.endpoint] = &endpointCredentials{Org: entry.Org}
		}
		return true, nil
	})
}

// newWhoamiCommand names the signed-in user.
func newWhoamiCommand() *cobra.Command {
	flags := &userFlags{}
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show who the CLI is signed in as",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newUserCommand(cmd, flags)
			if err != nil {
				return err
			}
			who, err := c.me(cmd.Context())
			if err != nil {
				return err
			}
			return c.render(who, []string{"NICKNAME", "GRN", "ENDPOINT"}, func() [][]string {
				return [][]string{{who.Nickname, who.Grn, c.endpoint}}
			})
		},
	}
	flags.bind(cmd)
	return cmd
}

func (c *userCommand) me(ctx context.Context) (*apigen.User, error) {
	client, err := c.signedIn(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := client.MeWithResponse(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "me", errorx.KindUnavailable)
	}
	if resp.JSON200 == nil {
		return nil, apiFailure(resp.HTTPResponse, resp.Body)
	}
	return resp.JSON200, nil
}
