# Archive smoke-test fixtures

All encrypted entries use password `password`.

- `password.7z`: generated with `7z a -t7z -mhe=on -ppassword`.
- `zip-crypto.zip`: generated with `7z a -tzip -ppassword -mem=ZipCrypto`.
- `zip-aes256.zip`: generated with `7z a -tzip -ppassword -mem=AES256`.

These contain `nested/hello.txt` with `hello archives\n`, an empty file,
and a Unicode filename. The ZIPs also contain a DEFLATE-compressed entry;
`zip-crypto.zip` includes an unencrypted entry.

`rar5-password.rar` is decoded unchanged from libarchive's
[`test_read_format_rar5_encrypted_filenames.rar.uu`](https://github.com/libarchive/libarchive/blob/master/libarchive/test/test_read_format_rar5_encrypted_filenames.rar.uu).
Its license is included in `COPYING.libarchive`.
