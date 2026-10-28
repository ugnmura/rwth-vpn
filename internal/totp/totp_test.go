package totp

import (
	"encoding/base32"
	"fmt"
	"testing"
	"time"
)

// RFC 6238 Appendix B; these are independent published test vectors.
func TestRFC6238(t *testing.T) {
	times := []int64{59, 1111111109, 1111111111, 1234567890, 2000000000, 20000000000}
	cases := []struct {
		algorithm, secret string
		want              []string
	}{
		{"SHA1", "12345678901234567890", []string{"94287082", "07081804", "14050471", "89005924", "69279037", "65353130"}},
		{"SHA256", "12345678901234567890123456789012", []string{"46119246", "68084774", "67062674", "91819424", "90698825", "77737706"}},
		{"SHA512", "1234567890123456789012345678901234567890123456789012345678901234", []string{"90693936", "25091201", "99943326", "93441116", "38618901", "47863826"}},
	}
	for _, tc := range cases {
		for i, now := range times {
			t.Run(fmt.Sprintf("%s/%d", tc.algorithm, now), func(t *testing.T) {
				uri := "otpauth://totp/test?secret=" + base32.StdEncoding.EncodeToString([]byte(tc.secret)) + "&digits=8&algorithm=" + tc.algorithm
				token, err := parseToken(uri)
				if err != nil {
					t.Fatal(err)
				}
				got, err := token.code(time.Unix(now, 0))
				if err != nil || got != tc.want[i] {
					t.Fatalf("got %q, %v; want %q", got, err, tc.want[i])
				}
			})
		}
	}
}

func TestRejectInvalidTokens(t *testing.T) {
	for _, raw := range []string{"", "not!base32", "otpauth://hotp/test?secret=JBSWY3DP", "otpauth://totp/test?secret=JBSWY3DP&digits=99", "otpauth://totp/test?secret=JBSWY3DP&period=0", "otpauth://totp/test?secret=JBSWY3DP&algorithm=MD5"} {
		if _, err := parseToken(raw); err == nil {
			t.Errorf("accepted invalid token %q", raw)
		}
	}
}

func TestPaddedAndUnpaddedTokens(t *testing.T) {
	a, err := parseToken("mzxw-6===")
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseToken("MZXW6")
	if err != nil {
		t.Fatal(err)
	}
	x, _ := a.code(time.Unix(59, 0))
	y, _ := b.code(time.Unix(59, 0))
	if x != y {
		t.Fatalf("padding changed code: %s != %s", x, y)
	}
}

func TestFreshCodesAreNotReused(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	token, err := parseToken("otpauth://totp/test?secret=JBSWY3DPEHPK3PXP&period=5")
	if err != nil {
		t.Fatal(err)
	}
	first, err := token.freshTime()
	if err != nil {
		t.Fatal(err)
	}
	second, err := token.freshTime()
	if err != nil {
		t.Fatal(err)
	}
	if second.Unix()/5 <= first.Unix()/5 {
		t.Fatal("reused a TOTP time step")
	}
}
