# Font provenance

Every file in this directory is an IBM Plex static instance under the SIL Open
Font License 1.1 (see `OFL.txt`). treekillbot is MIT; the OFL covers only these
font files, and shipping the licence alongside them is the whole obligation.

Static instances, not variable fonts: neither Go PDF library reads `fvar`, so a
`[wght]` variable font would embed one master, no bold, and a dead variation
table. See DESIGN.md D8.

IBM Plex Sans is taken from the IBM upstream rather than from `google/fonts`,
because `google/fonts/ofl/ibmplexsans` now carries only the variable
`IBMPlexSans[wdth,wght].ttf`. Mono and Serif statics are still published there.

Retrieved 2026-08-26.

## Sources

- Mono: `https://raw.githubusercontent.com/google/fonts/main/ofl/ibmplexmono/<file>`
- Serif: `https://raw.githubusercontent.com/google/fonts/main/ofl/ibmplexserif/<file>`
- Sans: `https://raw.githubusercontent.com/IBM/plex/master/packages/plex-sans/fonts/complete/ttf/<file>`
- Licence: `https://raw.githubusercontent.com/IBM/plex/master/LICENSE.txt`

## SHA-256

| File | Bytes | SHA-256 |
|---|---:|---|
| `IBMPlexMono-Bold.ttf` | 137784 | `ac27abd6450a64dd94467580a02fe6235156d5b92f2926ebbc8e7489df64e0be` |
| `IBMPlexMono-BoldItalic.ttf` | 144512 | `af4e05a761e98c1adf064c48a6352c9bec1a6ad70982cd2a544149323391f98e` |
| `IBMPlexMono-Italic.ttf` | 143920 | `3362fc791b0652193328b862c1c5f23a789bc7288b1617fa63302f88689a2a34` |
| `IBMPlexMono-Regular.ttf` | 135580 | `6a3412f058c7d8dfd9170c41e85ade48e5156ecb89356110ca57a0a27734af46` |
| `IBMPlexSans-Bold.ttf` | 200872 | `9e6c74a889a700d707613d24548fe4ffa6bc59559a0689d2cf9e133bdcdafb2f` |
| `IBMPlexSans-BoldItalic.ttf` | 208588 | `0e3142ba2ef31fe5c02f0c6c36424f251609cd6b73880076e21c2e81931ba2b9` |
| `IBMPlexSans-Italic.ttf` | 207920 | `a9c6ef9942c49e49d11e11a6dacc0b3a087978757e9b22a06b8ac22a6400fb15` |
| `IBMPlexSans-Regular.ttf` | 200500 | `975dcda37d80f038dcd143c22e33ca2d97a0cc5a929aace1c749153b0fe1afa5` |
| `IBMPlexSerif-Bold.ttf` | 163800 | `534c02c295999dd86e770457ece1d43db0de9256dd98bf741426f63ae904209e` |
| `IBMPlexSerif-BoldItalic.ttf` | 173032 | `2a32b76ac19c1942bf5942dbbd2a1566e5f1ae9833e421ebcf36d3522715e153` |
| `IBMPlexSerif-Italic.ttf` | 173004 | `4b75b38be4673527231f49c48818d090c913d5042dd5c747b525bf6185d29ecb` |
| `IBMPlexSerif-Regular.ttf` | 163068 | `e882efa9c41949a528ac2369079ec5ef050c1c996bbd0bacce3c3326d44cf80d` |
| `OFL.txt` | 4456 | `7e6b2818edbd8f6a01ae80641cc8f16a51080d08fb4e532be3a0b6f74adb07da` |
