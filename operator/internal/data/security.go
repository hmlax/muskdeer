package data

import "github.com/hmlax/muskdeer/deer/management/security"

type Authenticator = security.Authenticator
type UserTokenCache = security.UserTokenCache
type TokenChecker = security.TokenChecker

var NewAuthenticator = security.NewAuthenticator
var NewUserTokenCache = security.NewUserTokenCache
var NewTokenChecker = security.NewTokenChecker

type SessionMeta = security.SessionMeta

const RefreshTokenGraceWindow = security.RefreshTokenGraceWindow
