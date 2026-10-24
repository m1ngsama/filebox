// What the signed-in person may do, as /api/me reports it, so screens can leave out what the server would refuse.
export const access = $state<{ admin: boolean; levels: Record<string, string>; linkUploads: boolean }>({ admin: false, levels: {}, linkUploads: false })

export function setAccess(admin: boolean, levels: Record<string, string>, linkUploads = false) {
  access.admin = !!admin
  access.levels = levels ?? {}
  access.linkUploads = !!linkUploads
}

export const canWrite = (vol: string) => access.admin || access.levels[vol] === 'rw'
