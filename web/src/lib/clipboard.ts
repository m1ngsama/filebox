// Safari only lets a page write the clipboard inside the gesture, so the write must start before the text is known.
export function copyLater(text: Promise<string>): Promise<boolean> {
  try {
    if (typeof ClipboardItem !== 'undefined' && navigator.clipboard?.write)
      return navigator.clipboard.write([new ClipboardItem({ 'text/plain': text.then((s) => new Blob([s], { type: 'text/plain' })) })]).then(
        () => true,
        () => false,
      )
  } catch {}
  return text.then((s) => navigator.clipboard.writeText(s)).then(
    () => true,
    () => false,
  )
}
