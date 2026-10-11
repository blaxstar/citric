# winget

The release workflow submits each new version to
[microsoft/winget-pkgs](https://github.com/microsoft/winget-pkgs) with
`wingetcreate update BLAXSTAR.citric`, when the `WINGET_TOKEN` secret is set.

`wingetcreate update` needs the package to exist already. For the first
release, create it once by hand from the two MSIs:

    wingetcreate new https://github.com/blaxstar/citric/releases/download/v0.1.0/citric_0.1.0_windows_amd64.msi

with PackageIdentifier `BLAXSTAR.citric`, Publisher `BLAXSTAR LLC`, License
`Apache-2.0`, and submit the generated manifest.
