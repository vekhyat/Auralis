//! JSON output byte-compatible with Go's `encoding/json`.
//!
//! Go's encoder escapes `<`, `>`, `&`, U+2028 and U+2029 inside strings
//! (`<`, `>`, `&`, ` `, ` `); serde_json does not.
//! Files the Go app also reads or writes must go through these helpers so the
//! bytes match.

use serde::Serialize;

/// Equivalent of Go's `json.Marshal`.
pub fn to_vec<T: Serialize + ?Sized>(value: &T) -> serde_json::Result<Vec<u8>> {
    serde_json::to_vec(value).map(escape_html)
}

/// Equivalent of Go's `json.MarshalIndent(v, "", "  ")`.
pub fn to_vec_pretty<T: Serialize + ?Sized>(value: &T) -> serde_json::Result<Vec<u8>> {
    serde_json::to_vec_pretty(value).map(escape_html)
}

/// Applies Go's HTML-safe escaping to serialized JSON. These characters can
/// only occur inside JSON strings, so a byte-level pass is sufficient.
pub fn escape_html(json: Vec<u8>) -> Vec<u8> {
    let needs = json.iter().any(|b| matches!(b, b'<' | b'>' | b'&'))
        || json
            .windows(3)
            .any(|w| w == [0xE2, 0x80, 0xA8] || w == [0xE2, 0x80, 0xA9]);
    if !needs {
        return json;
    }
    let mut out = Vec::with_capacity(json.len() + 16);
    let mut i = 0;
    while i < json.len() {
        match json[i] {
            b'<' => out.extend_from_slice(br"\u003c"),
            b'>' => out.extend_from_slice(br"\u003e"),
            b'&' => out.extend_from_slice(br"\u0026"),
            0xE2 if json.get(i + 1) == Some(&0x80)
                && matches!(json.get(i + 2), Some(0xA8) | Some(0xA9)) =>
            {
                out.extend_from_slice(if json[i + 2] == 0xA8 {
                    br"\u2028"
                } else {
                    br"\u2029"
                });
                i += 3;
                continue;
            }
            b => out.push(b),
        }
        i += 1;
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn escapes_like_go() {
        let v = serde_json::json!({"u": "a<b>&c\u{2028}d\u{2029}é"});
        assert_eq!(
            String::from_utf8(to_vec(&v).unwrap()).unwrap(),
            r#"{"u":"a\u003cb\u003e\u0026c\u2028d\u2029é"}"#
        );
    }

    #[test]
    fn leaves_plain_json_untouched() {
        let v = serde_json::json!({"a": [1, "x"]});
        assert_eq!(
            to_vec_pretty(&v).unwrap(),
            serde_json::to_vec_pretty(&v).unwrap()
        );
    }
}
