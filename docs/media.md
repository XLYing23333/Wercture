# Media uploads

The server accepts JPEG, PNG, GIF, WebP, PDF, Markdown, MP4, WebM, MP3, M4A, AAC, WAV, OGG audio, and FLAC. Audio and video are stored unchanged. Playback depends on the browser's support for the original codec; the themed web player provides play/pause, seeking, volume, mute, video fullscreen, and a download action. Browser-native media controls are disabled in the web interface.

The server Docker image includes FFmpeg and FFprobe. Running the server directly requires both commands in `PATH`. Probing and video thumbnail generation only allow local file access, share two processing slots, and have a 60-second deadline per upload. Video thumbnails are PNG files with a maximum dimension of 640 pixels, extracted around 10% of the duration. Audio uses an embedded Bootstrap Icons music-note-beamed SVG; its MIT license is included at `/bootstrap-icons-LICENSE.txt`.

## Upload limit

An admin can set the maximum file size in Settings, in whole MiB. The default for both new and migrated databases is 1024 MiB (1 GiB, 1,073,741,824 bytes). The limit applies to every supported file type and both session and token uploads. Each upload snapshots the current limit when processing starts. Oversized uploads return HTTP 413 with `{"error":"toolarge"}`.

`GET /api/v1/settings` returns `locale`, `theme`, and `max_upload_bytes`. Admin-only `PUT /api/v1/settings` atomically updates settings; omitting `max_upload_bytes` preserves its current value. The byte value must be a positive integer no larger than 9,007,199,254,740,991. The web editor restricts values to whole MiB.

Original files stream to disk, and warehouse copies stream between files. Markdown preview input is limited to 256 KiB; UTF-8 validation covers the complete upload. PDF files up to 32 MiB use the first-page renderer; larger PDFs use a document placeholder while retaining the complete original file. Client-side image conversion can still require memory proportional to image size.

## Client

```sh
wercture upload --video clip.mp4
wercture upload --audio song.mp3
wercture upload --audio --link html song.flac
wercture upload --pdf document.pdf
wercture upload --md notes.md
```

`--video`, `--audio`, `--pdf`, and `--md` are mutually exclusive. Explicit image-processing flags (`--compress`, `--webp`, `--none`, `--quality`) cannot be combined with a file-type flag. Saved image processing preferences do not affect audio or video. A plain `upload` expects images. The TUI offers image, video, and audio choices before uploading files; image is the default.

Unprocessed uploads use streaming multipart requests. Uploads have a 60-minute timeout. Image processing remains internal to the Go client and does not require FFmpeg.

## Links and lifecycle

Direct links use `/i/{public_id}.{extension}` and return the original file with an inline disposition and its media MIME type. Extensionless links remain valid. GET, HEAD, and byte ranges are supported, including HTTP 206 for valid ranges and 416 for unsatisfiable ranges. Audio and video responses have no fixed total write deadline.

Audio and video Markdown copies are ordinary links. HTML copies use `<audio controls>` or `<video controls>`. File names and attributes are escaped.

Renaming and moving preserve the public ID and direct link. Copies receive new IDs and reuse video thumbnails. Trashed media and its video thumbnail return 404 publicly; the admin can preview them through authenticated endpoints. Restoring restores the original link. Permanent deletion removes the original and its thumbnail.
