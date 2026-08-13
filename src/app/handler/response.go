package handler

import "github.com/fastogt/fastoshop/app/httpjson"

// Aliases keep call sites short; the envelope lives in app/httpjson.
var (
	writeOK            = httpjson.WriteOK
	writeInternalError = httpjson.WriteInternalError
	writeBadRequest    = httpjson.WriteBadRequest
	writeUnauthorized  = httpjson.WriteUnauthorized
)
