// 密码哈希(bcrypt cost=12) + 强度校验
// 文档 §9.7 + §9.8
package auth

import "golang.org/x/crypto/bcrypt"

// HashPassword bcrypt(cost=12) 哈希
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword bcrypt 校验
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// VerifyPasswordStrength 校验密码强度
// 文档 §9.8:≥10 字符 + 大写 + 小写 + 数字 + 特殊字符
func VerifyPasswordStrength(p string) bool {
	if len(p) < 10 || len(p) > 32 {
		return false
	}
	hasLower, hasUpper, hasDigit, hasSpecial := false, false, false, false
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= '0' && r <= '9':
			hasDigit = true
		// 常见特殊字符
		case r == '!' || r == '@' || r == '#' || r == '$' || r == '%' ||
			r == '^' || r == '&' || r == '*' || r == '(' || r == ')' ||
			r == '-' || r == '_' || r == '=' || r == '+' || r == '[' ||
			r == ']' || r == '{' || r == '}' || r == '|' || r == '\\' ||
			r == ';' || r == ':' || r == '"' || r == '\'' || r == ',' ||
			r == '.' || r == '<' || r == '>' || r == '/' || r == '?' ||
			r == '`' || r == '~':
			hasSpecial = true
		}
	}
	return hasLower && hasUpper && hasDigit && hasSpecial
}
