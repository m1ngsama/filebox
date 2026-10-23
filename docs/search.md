# Search and duplicates

## Content search

filebox extracts the text inside documents in the background so search also finds words in them. It reads up to 1 MiB of text from each plain-text, Markdown, code, HTML, SVG, XML and EPUB file, and from PDFs when poppler's [`pdftotext`](https://poppler.freedesktop.org/) is on the `PATH`. Content matches need at least 3 characters, except Chinese, Japanese and Korean text, which a separate bigram index lets match from 1 character. On Linux the extraction threads and `pdftotext` run at nice 19; elsewhere only `pdftotext` does. A file that fails to extract is retried once after 24 hours.

The index lives in `content.db` in the data directory and takes 2.5 to 4 times the extracted text, so a library of 500 books needs 1 to 2 GB. content.db can be deleted at any time and is rebuilt in the background. Pass `-content=false` to stop indexing and ignore the file.

## Text in images

With `-ocr`, filebox also reads the words in PNG, JPEG, WebP, TIFF, BMP and GIF images with [tesseract](https://github.com/tesseract-ocr/tesseract), so screenshots and scans turn up in search. It uses whichever of English, Simplified and Traditional Chinese, Japanese and Korean tesseract has installed (`tesseract --list-langs`), skips images over 32 MiB, and keeps nothing from an image with fewer than three words in it. Reading a large photo library this way takes hours of CPU, at nice 19, which is why it is off by default.

## Duplicates

The duplicate finder (`/duplicates`) hashes only files that share a size with another file, remembers each hash in the database until the file changes, and works through a large library over several rounds of 8 seconds. Selected copies go to the trash, and at least one copy in each group always stays.
