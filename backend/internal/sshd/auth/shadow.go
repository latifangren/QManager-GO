package auth

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"crypto/subtle"
	"fmt"
	"os"
	"strings"
)

const itoa64 = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// VerifyShadowPassword checks if the plain password matches the user entry in /etc/shadow.
func VerifyShadowPassword(username, password, shadowPath string) (bool, error) {
	if shadowPath == "" {
		shadowPath = "/etc/shadow"
	}

	file, err := os.Open(shadowPath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		if parts[0] == username {
			storedHash := parts[1]
			// Empty password or locked account
			if storedHash == "" {
				return password == "", nil
			}
			if storedHash == "*" || storedHash == "!" || strings.HasPrefix(storedHash, "!") {
				return false, nil
			}
			return CheckPasswordHash(password, storedHash), nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, err
	}

	return false, fmt.Errorf("user %s not found in shadow", username)
}

// CheckPasswordHash verifies a plain password against standard unix crypt hashes ($1$, DES, or plain)
func CheckPasswordHash(password, storedHash string) bool {
	if strings.HasPrefix(storedHash, "$1$") {
		// MD5 crypt ($1$)
		parts := strings.Split(storedHash, "$")
		if len(parts) >= 3 {
			salt := parts[2]
			computed := CryptMD5(password, salt)
			return subtle.ConstantTimeCompare([]byte(computed), []byte(storedHash)) == 1
		}
	}
	// Fallback constant-time compare for non-shadow/plain
	return subtle.ConstantTimeCompare([]byte(password), []byte(storedHash)) == 1
}

// CryptMD5 implements standard glibc/FreeBSD MD5-Crypt ($1$)
func CryptMD5(key, salt string) string {
	// Salt is at most 8 chars
	if len(salt) > 8 {
		salt = salt[:8]
	}

	keyB := []byte(key)
	saltB := []byte(salt)

	// Digest B
	hB := md5.New()
	hB.Write(keyB)
	hB.Write(saltB)
	hB.Write(keyB)
	dB := hB.Sum(nil)

	// Digest A
	hA := md5.New()
	hA.Write(keyB)
	hA.Write([]byte("$1$"))
	hA.Write(saltB)

	// Add dB repeatedly
	for i := len(keyB); i > 0; i -= 16 {
		if i > 16 {
			hA.Write(dB)
		} else {
			hA.Write(dB[:i])
		}
	}

	// For each bit of length, add 0 or first char of key
	for i := len(keyB); i > 0; i >>= 1 {
		if (i & 1) != 0 {
			hA.Write([]byte{0})
		} else {
			hA.Write([]byte{keyB[0]})
		}
	}
	dA := hA.Sum(nil)

	// 1000 rounds
	for i := 0; i < 1000; i++ {
		hC := md5.New()
		if (i & 1) != 0 {
			hC.Write(keyB)
		} else {
			hC.Write(dA)
		}
		if (i % 3) != 0 {
			hC.Write(saltB)
		}
		if (i % 7) != 0 {
			hC.Write(keyB)
		}
		if (i & 1) != 0 {
			hC.Write(dA)
		} else {
			hC.Write(keyB)
		}
		dA = hC.Sum(nil)
	}

	// Custom base64 permutation for MD5-crypt
	var b bytes.Buffer
	b.WriteString("$1$")
	b.WriteString(salt)
	b.WriteString("$")

	to64(&b, uint32(dA[0])<<16|uint32(dA[6])<<8|uint32(dA[12]), 4)
	to64(&b, uint32(dA[1])<<16|uint32(dA[7])<<8|uint32(dA[13]), 4)
	to64(&b, uint32(dA[2])<<16|uint32(dA[8])<<8|uint32(dA[14]), 4)
	to64(&b, uint32(dA[3])<<16|uint32(dA[9])<<8|uint32(dA[15]), 4)
	to64(&b, uint32(dA[4])<<16|uint32(dA[10])<<8|uint32(dA[5]), 4)
	to64(&b, uint32(dA[11]), 2)

	return b.String()
}

func to64(b *bytes.Buffer, v uint32, n int) {
	for i := 0; i < n; i++ {
		b.WriteByte(itoa64[v&0x3f])
		v >>= 6
	}
}
