package domain

// ExtractedSecret is one payload pulled out of AWS Secrets Manager. AWS
// returns a secret's value in exactly one of SecretString or SecretBinary,
// so at most one of String/Binary is populated here - both are pointers so
// "absent" (nil) is distinguishable from "present but empty".
type ExtractedSecret struct {
	Name   string
	String *string
	Binary *string
}
