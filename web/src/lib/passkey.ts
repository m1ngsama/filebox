import type { WebAuthnError } from '@simplewebauthn/browser'
import { api, HttpError } from './api'
import { t } from './i18n'

export const webauthn = () => import('@simplewebauthn/browser')

export async function passkeyLogin(autofill = false) {
  const [{ ceremony, options }, { startAuthentication }] = await Promise.all([api.passkeyLoginBegin(), webauthn()])
  await api.passkeyLoginFinish(ceremony, await startAuthentication({ optionsJSON: options, useBrowserAutofill: autofill }))
}

export async function addPasskey(name: string) {
  const [{ ceremony, options }, { startRegistration }] = await Promise.all([api.passkeyRegisterBegin(), webauthn()])
  await api.passkeyRegisterFinish(ceremony, name, await startRegistration({ optionsJSON: options }))
}

export function passkeyError(e: unknown) {
  if (e instanceof HttpError) return e.status === 429 ? e.message : t.passkeyInvalid
  const { name, code } = e as WebAuthnError
  if (name === 'NotAllowedError' || name === 'AbortError' || code === 'ERROR_CEREMONY_ABORTED') return t.passkeyCancelled
  if (code === 'ERROR_AUTHENTICATOR_PREVIOUSLY_REGISTERED') return t.passkeyExists
  if (typeof PublicKeyCredential !== 'function' || name === 'NotSupportedError' || name === 'SecurityError') return t.passkeyUnsupported
  return t.passkeyInvalid
}

export const validPasskeyName = (n: string) => [...n].length <= 64

const devices = ['iPhone', 'iPad', 'Android', 'Mac', 'Windows', 'Linux']

export function deviceName() {
  const d = devices.find((n) => navigator.userAgent.includes(n)) ?? ''
  return d === 'Mac' ? 'MacBook' : d
}
