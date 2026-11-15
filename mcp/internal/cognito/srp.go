package cognito

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// nHex is the 3072-bit group prime from RFC 5054 that Cognito uses for SRP.
const nHex = "FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD1" +
	"29024E088A67CC74020BBEA63B139B22514A08798E3404DD" +
	"EF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245" +
	"E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED" +
	"EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3D" +
	"C2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F" +
	"83655D23DCA3AD961C62F356208552BB9ED529077096966D" +
	"670C354E4ABC9804F1746C08CA18217C32905E462E36CE3B" +
	"E39E772C180E86039B2783A2EC07A28FB5C55DF06F4C52C9" +
	"DE2BCBF6955817183995497CEA956AE515D2261898FA0510" +
	"15728E5A8AAAC42DAD33170D04507A33A85521ABDF1CBA64" +
	"ECFB850458DBEF0A8AEA71575D060C7DB3970F85A6E1E4C7" +
	"ABF5AE8CDB0933D71E8C94E04A25619DCEE3D2261AD2EE6B" +
	"F12FFA06D98A0864D87602733EC86A64521F2B18177B200C" +
	"BBE117577A615D6C770988C0BAD946E208E24FA074E5AB31" +
	"43DB5BFCE0FD108E4B82D120A93AD2CAFFFFFFFFFFFFFFFF"

var (
	srpN, _ = new(big.Int).SetString(nHex, 16)
	srpG    = big.NewInt(2)
	srpK    = hashHexToInt(padHex(srpN) + padHex(srpG))
)

// srpClient holds the ephemeral client secret a and public value A of one
// USER_SRP_AUTH sign-in.
type srpClient struct {
	poolName string // user pool ID without the "<region>_" prefix
	a        *big.Int
	A        *big.Int
}

func newSRPClient(userPoolID string) (*srpClient, error) {
	_, poolName, ok := strings.Cut(userPoolID, "_")
	if !ok || poolName == "" {
		return nil, fmt.Errorf("invalid user pool ID %q: expected <region>_<id>", userPoolID)
	}
	buf := make([]byte, 128)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	a := new(big.Int).SetBytes(buf)
	A := new(big.Int).Exp(srpG, a, srpN)
	if new(big.Int).Mod(A, srpN).Sign() == 0 {
		return nil, errors.New("SRP: A mod N is zero")
	}
	return &srpClient{poolName: poolName, a: a, A: A}, nil
}

// srpAHex is the SRP_A auth parameter.
func (c *srpClient) srpAHex() string { return c.A.Text(16) }

// passwordVerifier computes the PASSWORD_VERIFIER challenge responses from
// the challenge parameters Cognito returned for USER_SRP_AUTH.
func (c *srpClient) passwordVerifier(params map[string]string, password string, now time.Time) (map[string]string, error) {
	userID := params["USER_ID_FOR_SRP"]
	if userID == "" {
		return nil, errors.New("SRP: challenge is missing USER_ID_FOR_SRP")
	}
	B, ok := new(big.Int).SetString(params["SRP_B"], 16)
	if !ok {
		return nil, errors.New("SRP: challenge has invalid SRP_B")
	}
	salt, ok := new(big.Int).SetString(params["SALT"], 16)
	if !ok {
		return nil, errors.New("SRP: challenge has invalid SALT")
	}
	secretBlock, err := base64.StdEncoding.DecodeString(params["SECRET_BLOCK"])
	if err != nil {
		return nil, errors.New("SRP: challenge has invalid SECRET_BLOCK")
	}

	key, err := c.authenticationKey(userID, password, B, salt)
	if err != nil {
		return nil, err
	}

	timestamp := formatTimestamp(now)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(c.poolName))
	mac.Write([]byte(userID))
	mac.Write(secretBlock)
	mac.Write([]byte(timestamp))

	return map[string]string{
		"USERNAME":                    userID,
		"PASSWORD_CLAIM_SECRET_BLOCK": params["SECRET_BLOCK"],
		"TIMESTAMP":                   timestamp,
		"PASSWORD_CLAIM_SIGNATURE":    base64.StdEncoding.EncodeToString(mac.Sum(nil)),
	}, nil
}

// authenticationKey derives the 16-byte HKDF key from the shared secret S.
func (c *srpClient) authenticationKey(userID, password string, B, salt *big.Int) ([]byte, error) {
	if new(big.Int).Mod(B, srpN).Sign() == 0 {
		return nil, errors.New("SRP: B mod N is zero")
	}
	u := hashHexToInt(padHex(c.A) + padHex(B))
	if u.Sign() == 0 {
		return nil, errors.New("SRP: u is zero")
	}
	x := passwordHash(c.poolName, userID, password, salt)

	// S = (B - k * g^x) ^ (a + u*x) mod N
	gx := new(big.Int).Exp(srpG, x, srpN)
	base := new(big.Int).Sub(B, new(big.Int).Mul(srpK, gx))
	base.Mod(base, srpN)
	exp := new(big.Int).Add(c.a, new(big.Int).Mul(u, x))
	S := new(big.Int).Exp(base, exp, srpN)

	return hkdf(mustHex(padHex(S)), mustHex(padHex(u))), nil
}

// passwordHash is x = H(pad(salt) | H(poolName | userID | ":" | password)).
func passwordHash(poolName, userID, password string, salt *big.Int) *big.Int {
	inner := sha256.Sum256([]byte(poolName + userID + ":" + password))
	return hashHexToInt(padHex(salt) + hex.EncodeToString(inner[:]))
}

// hkdf is the single-block HKDF-SHA256 with info "Caldera Derived Key".
func hkdf(ikm, salt []byte) []byte {
	prk := hmac.New(sha256.New, salt)
	prk.Write(ikm)
	okm := hmac.New(sha256.New, prk.Sum(nil))
	okm.Write([]byte("Caldera Derived Key\x01"))
	return okm.Sum(nil)[:16]
}

// formatTimestamp renders the time the way Cognito expects in TIMESTAMP,
// e.g. "Tue Sep 5 13:05:07 UTC 2026" (day of month not padded).
func formatTimestamp(t time.Time) string {
	return t.UTC().Format("Mon Jan 2 15:04:05 UTC 2006")
}

// padHex returns the even-length hex form of a non-negative integer, with a
// leading "00" when the high bit is set so it reads as positive.
func padHex(n *big.Int) string {
	s := n.Text(16)
	if len(s)%2 == 1 {
		s = "0" + s
	}
	if strings.ContainsRune("89abcdef", rune(s[0])) {
		s = "00" + s
	}
	return s
}

func hashHexToInt(h string) *big.Int {
	sum := sha256.Sum256(mustHex(h))
	return new(big.Int).SetBytes(sum[:])
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}
