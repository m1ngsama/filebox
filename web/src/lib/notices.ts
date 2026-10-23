import { selectURL, type Notice } from './api'
import { size } from './format'
import { t } from './i18n'

export function noticeText(n: Notice) {
  const f = t.noticeText[n.kind]
  if (!f) return n.kind
  return n.kind === 'disk_low' ? f(n.vol, size(n.count)) : f(n.name, n.share)
}

// Where a notice leads when someone opens it.
export function noticeLink(n: Notice) {
  switch (n.kind) {
    case 'share_upload':
      return n.vol && n.path ? selectURL(n.vol, n.path) : '/shares'
    case 'share_download':
      return '/shares'
    case 'joined':
      return '/team'
    case 'disk_low':
      return '/storage'
  }
  return '/'
}
