import { browserSupportsWebAuthn, startAuthentication, startRegistration, type WebAuthnError } from '@simplewebauthn/browser'
import { api, HttpError } from './api'
import { t } from './i18n'

export async function passkeyLogin(autofill = false) {
  const { ceremony, options } = await api.passkeyLoginBegin()
  await api.passkeyLoginFinish(ceremony, await startAuthentication({ optionsJSON: options, useBrowserAutofill: autofill }))
}

export async function addPasskey(name: string) {
  const { ceremony, options } = await api.passkeyRegisterBegin()
  await api.passkeyRegisterFinish(ceremony, name, await startRegistration({ optionsJSON: options }))
}

export function passkeyError(e: unknown) {
  if (e instanceof HttpError) return e.status === 429 ? t.tooMany : t.passkeyInvalid
  const { name, code } = e as WebAuthnError
  if (name === 'NotAllowedError' || name === 'AbortError' || code === 'ERROR_CEREMONY_ABORTED') return t.passkeyCancelled
  if (code === 'ERROR_AUTHENTICATOR_PREVIOUSLY_REGISTERED') return t.passkeyExists
  if (!browserSupportsWebAuthn() || name === 'NotSupportedError' || name === 'SecurityError') return t.passkeyUnsupported
  return t.passkeyInvalid
}

export const validPasskeyName = (n: string) => [...n].length <= 64

const devices = ['iPhone', 'iPad', 'Android', 'Mac', 'Windows', 'Linux']

export function deviceName() {
  const d = devices.find((n) => navigator.userAgent.includes(n)) ?? ''
  return d === 'Mac' ? 'MacBook' : d
}
