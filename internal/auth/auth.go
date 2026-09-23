package auth

type KeyStore interface {
	Issue(proberName string) string
	Verify(proberName, key string) bool
}
