//! Port of `backend/mp4ff_decrypt.go`.
//!
//! Decryption follows mp4ff v0.55.0: parse the fragmented file, decrypt sample
//! bytes in place, drop encryption boxes, and re-encode init plus segments.
//! Unmodified boxes keep their original bytes so the result matches Go.

use aes::cipher::{BlockDecrypt, BlockEncrypt, KeyInit};
use aes::Aes128;
use base64::engine::general_purpose::STANDARD;
use base64::Engine;
use std::collections::HashMap;
use std::fs::File;
use std::io::Write;
use std::path::Path;

const MAX_DEPTH: u32 = 32;
const PIFF_SENC: [u8; 16] = [
    0xa2, 0x39, 0x4f, 0x52, 0x5a, 0x9b, 0x4f, 0x14, 0xa2, 0x44, 0x6c, 0x42, 0x7c, 0x64, 0x8d, 0xf4,
];
const PIFF_TENC: [u8; 16] = [
    0x89, 0x74, 0xdb, 0xce, 0x7b, 0xe7, 0x4c, 0x51, 0x84, 0xf9, 0x71, 0x48, 0xf9, 0x88, 0x25, 0x54,
];

#[derive(Debug)]
pub struct Mp4Error {
    message: String,
}

impl Mp4Error {
    fn new(message: impl Into<String>) -> Self {
        Self {
            message: message.into(),
        }
    }
}

impl std::fmt::Display for Mp4Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.message)
    }
}

impl std::error::Error for Mp4Error {}

#[derive(Debug)]
struct KeyMaterial {
    legacy: Option<Vec<u8>>,
    by_kid: HashMap<String, Vec<u8>>,
    strict: bool,
}

#[derive(Clone)]
struct TencInfo {
    crypt: u8,
    skip: u8,
    iv_size: u8,
    kid: [u8; 16],
    constant_iv: Vec<u8>,
}

#[derive(Clone)]
struct TrexInfo {
    track_id: u32,
    default_size: u32,
}

struct TrackInfo {
    track_id: u32,
    scheme: String,
    tenc: Option<TencInfo>,
    trex: Option<TrexInfo>,
}

#[derive(Clone)]
enum BoxKind {
    Leaf,
    Children(Vec<IsoBox>),
    Prefixed {
        prefix_len: usize,
        trailing: Vec<u8>,
        children: Vec<IsoBox>,
    },
}

#[derive(Clone)]
struct IsoBox {
    fourcc: [u8; 4],
    start: u64,
    header_len: usize,
    raw: Vec<u8>,
    kind: BoxKind,
    drop: bool,
    dirty: bool,
    rename: Option<[u8; 4]>,
    payload_override: Option<Vec<u8>>,
}

struct Frag {
    boxes: Vec<IsoBox>,
}

struct Seg {
    styp: Option<IsoBox>,
    frags: Vec<Frag>,
}

struct Mp4Doc {
    init: Vec<IsoBox>,
    segments: Vec<Seg>,
    fragmented: bool,
}

struct ParsedSenc {
    ivs: Vec<Vec<u8>>,
    subs: Vec<Vec<(u16, u32)>>,
}

struct TfhdInfo {
    flags: u32,
    track_id: u32,
    base_data_offset: u64,
    default_duration: u32,
    default_size: u32,
    default_flags: u32,
}

struct TrunSample {
    size: u32,
}

struct TrunInfo {
    data_offset: Option<i32>,
    samples: Vec<TrunSample>,
}

/// Decrypt `input` with mp4ff key specs and write the re-encoded file to `output`.
/// Key-parse errors are returned as-is. Decrypt failures are prefixed with
/// `mp4ff decryption failed:` and the output file is removed.
pub fn decrypt_with_mp4ff(
    key_specs: &[String],
    input: &Path,
    output: &Path,
) -> Result<(), Mp4Error> {
    let keys = parse_mp4ff_key_specs(key_specs)?;
    let data = std::fs::read(input)
        .map_err(|err| Mp4Error::new(format!("failed to open encrypted MP4: {err}")))?;
    let mut file = File::create(output)
        .map_err(|err| Mp4Error::new(format!("failed to create decrypted MP4: {err}")))?;
    let decrypted = match decrypt_bytes(&data, &keys) {
        Ok(bytes) => bytes,
        Err(err) => {
            drop(file);
            let _ = std::fs::remove_file(output);
            return Err(Mp4Error::new(format!("mp4ff decryption failed: {err}")));
        }
    };
    if let Err(err) = file.write_all(&decrypted).and_then(|_| file.flush()) {
        drop(file);
        let _ = std::fs::remove_file(output);
        return Err(Mp4Error::new(format!(
            "failed to finalize decrypted MP4: {err}"
        )));
    }
    Ok(())
}

fn decrypt_bytes(data: &[u8], keys: &KeyMaterial) -> Result<Vec<u8>, Mp4Error> {
    let mut doc = parse_file(data)?;
    if !doc.fragmented {
        return Err(Mp4Error::new("file not fragmented. Not supported"));
    }
    if !doc.init.iter().any(|b| b.fourcc == *b"moov") {
        return Err(Mp4Error::new(
            "no init segment file and no init part of file",
        ));
    }
    let tracks = strip_init(&mut doc.init)?;
    for seg in &doc.segments {
        for frag in &seg.frags {
            parse_fragment_sencs(frag, &tracks)?;
        }
    }
    for seg in &mut doc.segments {
        decrypt_segment(seg, &tracks, keys)?;
    }
    encode_doc(&mut doc)
}

fn parse_mp4ff_key_specs(key_specs: &[String]) -> Result<KeyMaterial, Mp4Error> {
    let mut normalized_specs = Vec::new();
    let mut seen = std::collections::HashSet::new();
    for spec in key_specs {
        let normalized = normalize_mp4ff_key_spec(spec)?;
        if normalized.is_empty() || !seen.insert(normalized.clone()) {
            continue;
        }
        normalized_specs.push(normalized);
    }
    if normalized_specs.is_empty() {
        return Err(Mp4Error::new("no mp4ff key specs provided"));
    }
    let has_kid = normalized_specs.iter().any(|s| s.contains(':'));
    let has_legacy = normalized_specs.iter().any(|s| !s.contains(':'));
    if has_kid && has_legacy {
        return Err(Mp4Error::new(
            "cannot mix legacy key and kid:key key format",
        ));
    }
    if !has_kid {
        if normalized_specs.len() != 1 {
            return Err(Mp4Error::new("multiple legacy keys are not supported"));
        }
        let key = unpack_key(&normalized_specs[0])
            .map_err(|err| Mp4Error::new(format!("unpacking key: {err}")))?;
        return Ok(KeyMaterial {
            legacy: Some(key),
            by_kid: HashMap::new(),
            strict: false,
        });
    }
    let mut by_kid = HashMap::new();
    for spec in &normalized_specs {
        let (left, right) = split_once_colon(spec)
            .ok_or_else(|| Mp4Error::new(format!("bad kid:key format {}", go_quote(spec))))?;
        let left = left.trim();
        let right = right.trim();
        if left.is_empty() || right.is_empty() {
            return Err(Mp4Error::new(format!(
                "bad kid:key format {}",
                go_quote(spec)
            )));
        }
        let kid = unpack_key(left).map_err(|err| Mp4Error::new(format!("unpacking kid: {err}")))?;
        let kid_hex = hex::encode(&kid);
        if by_kid.contains_key(&kid_hex) {
            return Err(Mp4Error::new(format!("duplicate kid {kid_hex}")));
        }
        let parsed = unpack_key(right)
            .map_err(|err| Mp4Error::new(format!("unpacking key for kid {kid_hex}: {err}")))?;
        by_kid.insert(kid_hex, parsed);
    }
    Ok(KeyMaterial {
        legacy: None,
        by_kid,
        strict: true,
    })
}

fn normalize_mp4ff_key_spec(spec: &str) -> Result<String, Mp4Error> {
    let spec = spec.trim();
    if spec.is_empty() || !spec.contains(':') {
        return Ok(spec.to_string());
    }
    let (left, right) = split_once_colon(spec).unwrap_or(("", ""));
    let left = left.trim();
    let right = right.trim();
    if left.is_empty() || right.is_empty() {
        return Err(Mp4Error::new(format!("bad key spec {}", go_quote(spec))));
    }
    if unpack_key(left).is_ok() {
        return Ok(format!("{left}:{right}"));
    }
    if !is_decimal_string(left) {
        return Err(Mp4Error::new(format!(
            "bad kid in key spec {}",
            go_quote(spec)
        )));
    }
    if let Err(err) = unpack_key(right) {
        return Err(Mp4Error::new(format!(
            "bad key spec {}: {err}",
            go_quote(spec)
        )));
    }
    Ok(right.to_string())
}

fn is_decimal_string(value: &str) -> bool {
    !value.is_empty() && value.bytes().all(|ch| ch.is_ascii_digit())
}

fn split_once_colon(spec: &str) -> Option<(&str, &str)> {
    spec.split_once(':')
}

fn unpack_key(in_key: &str) -> Result<Vec<u8>, String> {
    let bytes = in_key.as_bytes();
    match bytes.len() {
        36 => {
            if bytes[8] != b'-' || bytes[13] != b'-' || bytes[18] != b'-' || bytes[23] != b'-' {
                return Err(format!("bad uuid format: {}", shorten(in_key)));
            }
            let stripped: String = in_key.chars().filter(|ch| *ch != '-').collect();
            if stripped.len() != 32 {
                return Err(format!("bad uuid format: {}", shorten(&stripped)));
            }
            hex_decode(&stripped).map_err(|err| format!("bad uuid {}: {err}", shorten(&stripped)))
        }
        32 => hex_decode(in_key).map_err(|err| format!("bad hex {}: {err}", shorten(in_key))),
        24 => STANDARD
            .decode(in_key)
            .map_err(|err| format!("bad base64 {}: {err}", shorten(in_key))),
        _ => Err(format!("cannot decode key {in_key}")),
    }
}

fn shorten(value: &str) -> String {
    let count = value.chars().count();
    if count < 12 {
        return value.to_string();
    }
    let head: String = value.chars().take(6).collect();
    let tail: String = value.chars().skip(count - 6).collect();
    format!("{head}...{tail}")
}

fn go_quote(value: &str) -> String {
    let mut out = String::from("\"");
    for ch in value.chars() {
        match ch {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 || c == '\u{007f}' => {
                out.push_str(&format!("\\x{:02x}", c as u32));
            }
            c => out.push(c),
        }
    }
    out.push('"');
    out
}

fn hex_decode(value: &str) -> Result<Vec<u8>, String> {
    if !value.len().is_multiple_of(2) {
        return Err("encoding/hex: odd length hex string".to_string());
    }
    let bytes = value.as_bytes();
    let mut out = Vec::with_capacity(bytes.len() / 2);
    let mut i = 0;
    while i < bytes.len() {
        let hi = hex_val(bytes[i])?;
        let lo = hex_val(bytes[i + 1])?;
        out.push((hi << 4) | lo);
        i += 2;
    }
    Ok(out)
}

fn hex_val(byte: u8) -> Result<u8, String> {
    match byte {
        b'0'..=b'9' => Ok(byte - b'0'),
        b'a'..=b'f' => Ok(byte - b'a' + 10),
        b'A'..=b'F' => Ok(byte - b'A' + 10),
        _ => Err(format!("encoding/hex: invalid byte: {}", go_sharp_u(byte))),
    }
}

fn go_sharp_u(byte: u8) -> String {
    let ch = byte as char;
    if byte < 0x20 || byte == 0x7f {
        format!("U+{byte:04X}")
    } else {
        format!("U+{byte:04X} '{ch}'")
    }
}

fn parse_file(data: &[u8]) -> Result<Mp4Doc, Mp4Error> {
    let mut doc = Mp4Doc {
        init: Vec::new(),
        segments: Vec::new(),
        fragmented: false,
    };
    let mut ftyp: Option<IsoBox> = None;
    let mut last = [0u8; 4];
    let mut have_last = false;
    let mut off = 0usize;
    while off < data.len() {
        let abs = off as u64;
        let (box_value, n) = parse_box(&data[off..], abs, 0, None)?;
        off += n;
        let typ = box_value.fourcc;
        if typ == *b"mdat" && doc.fragmented {
            let previous = if have_last {
                fourcc_str(&last)
            } else {
                String::new()
            };
            if last != *b"moof" {
                return Err(Mp4Error::new(format!(
                    "does not support {previous} between moof and mdat"
                )));
            }
        }
        match &typ {
            b"ftyp" => ftyp = Some(box_value),
            b"moov" => {
                if stts_entry_count(&box_value)? == Some(0) {
                    doc.fragmented = true;
                    if let Some(ft) = ftyp.take() {
                        doc.init.push(ft);
                    }
                    doc.init.push(box_value);
                }
            }
            b"styp" => {
                doc.fragmented = true;
                doc.segments.push(Seg {
                    styp: Some(box_value),
                    frags: Vec::new(),
                });
            }
            b"sidx" => {
                if have_last && last == *b"mdat" {
                    doc.fragmented = true;
                    doc.segments.push(Seg {
                        styp: None,
                        frags: Vec::new(),
                    });
                }
            }
            b"emsg" => {
                ensure_segment(&mut doc, abs);
                let seg = doc
                    .segments
                    .last_mut()
                    .ok_or_else(|| Mp4Error::new("emsg without segment"))?;
                if seg.frags.is_empty() {
                    seg.frags.push(Frag { boxes: Vec::new() });
                }
                if let Some(frag) = seg.frags.last_mut() {
                    frag.boxes.push(box_value);
                }
            }
            b"moof" => {
                doc.fragmented = true;
                ensure_segment(&mut doc, abs);
                let seg = doc
                    .segments
                    .last_mut()
                    .ok_or_else(|| Mp4Error::new("moof without segment"))?;
                let needs_new = match seg.frags.last() {
                    None => true,
                    Some(frag) => frag.boxes.iter().any(|b| b.fourcc == *b"moof"),
                };
                if needs_new {
                    seg.frags.push(Frag { boxes: Vec::new() });
                }
                if let Some(frag) = seg.frags.last_mut() {
                    frag.boxes.push(box_value);
                }
            }
            b"mdat" if doc.fragmented => {
                if let Some(seg) = doc.segments.last_mut() {
                    if let Some(frag) = seg.frags.last_mut() {
                        frag.boxes.push(box_value);
                    }
                }
            }
            _ => {}
        }
        last = typ;
        have_last = true;
    }
    Ok(doc)
}

fn ensure_segment(doc: &mut Mp4Doc, abs: u64) {
    if doc.segments.is_empty() {
        let _ = abs;
        doc.fragmented = true;
        doc.segments.push(Seg {
            styp: None,
            frags: Vec::new(),
        });
    }
}

fn parse_box(
    data: &[u8],
    abs: u64,
    depth: u32,
    parent: Option<[u8; 4]>,
) -> Result<(IsoBox, usize), Mp4Error> {
    if depth > MAX_DEPTH {
        return Err(Mp4Error::new("mp4 box nesting exceeds 32"));
    }
    let (size, header_len, fourcc) = read_header(data)?;
    let size_us =
        usize::try_from(size).map_err(|_| Mp4Error::new("box size exceeds address space"))?;
    if size_us > data.len() {
        return Err(Mp4Error::new(format!(
            "box extends past end: size {size} remaining {}",
            data.len()
        )));
    }
    let raw = data[..size_us].to_vec();
    let payload = &raw[header_len..];
    if fourcc == *b"uuid" {
        check_piff_senc(&raw, size)?;
    }
    let kind = match box_mode(parent, fourcc) {
        Mode::Leaf => BoxKind::Leaf,
        Mode::Kids => {
            let children = parse_region(payload, abs + header_len as u64, depth + 1, fourcc)?;
            BoxKind::Children(children)
        }
        Mode::Stsd => {
            if payload.len() < 8 {
                return Err(Mp4Error::new("stsd payload is short"));
            }
            let children = parse_region(
                &payload[8..],
                abs + header_len as u64 + 8,
                depth + 1,
                fourcc,
            )?;
            BoxKind::Prefixed {
                prefix_len: 8,
                trailing: Vec::new(),
                children,
            }
        }
        Mode::Audio => {
            if payload.len() < 28 {
                return Err(Mp4Error::new(format!(
                    "bad size when decoding {}",
                    fourcc_str(&fourcc)
                )));
            }
            let children = parse_region(
                &payload[28..],
                abs + header_len as u64 + 28,
                depth + 1,
                fourcc,
            )?;
            BoxKind::Prefixed {
                prefix_len: 28,
                trailing: Vec::new(),
                children,
            }
        }
        Mode::Visual => parse_visual(payload, abs, header_len, depth, fourcc)?,
    };
    Ok((
        IsoBox {
            fourcc,
            start: abs,
            header_len,
            raw,
            kind,
            drop: false,
            dirty: false,
            rename: None,
            payload_override: None,
        },
        size_us,
    ))
}

fn parse_region(
    region: &[u8],
    abs: u64,
    depth: u32,
    parent: [u8; 4],
) -> Result<Vec<IsoBox>, Mp4Error> {
    let mut children = Vec::new();
    let mut off = 0usize;
    while off < region.len() {
        let (child, n) = parse_box(&region[off..], abs + off as u64, depth, Some(parent))
            .map_err(|err| decode_wrap(&parent, abs.saturating_sub(8), err))?;
        if n == 0 || off + n > region.len() {
            return Err(Mp4Error::new(children_size_msg(region.len(), &children)));
        }
        off += n;
        children.push(child);
    }
    Ok(children)
}

fn parse_visual(
    payload: &[u8],
    abs: u64,
    header_len: usize,
    depth: u32,
    fourcc: [u8; 4],
) -> Result<BoxKind, Mp4Error> {
    if payload.len() < 78 {
        return Err(Mp4Error::new(format!(
            "bad size when decoding {}",
            fourcc_str(&fourcc)
        )));
    }
    let mut children = Vec::new();
    let mut off = 78usize;
    let mut trailing = Vec::new();
    while off < payload.len() {
        let rest = payload.len() - off;
        if rest < 8 {
            trailing = payload[off..].to_vec();
            break;
        }
        let child_abs = abs + header_len as u64 + off as u64;
        let (child, n) = parse_box(&payload[off..], child_abs, depth + 1, Some(fourcc))?;
        if n == 0 || off + n > payload.len() {
            return Err(Mp4Error::new(children_size_msg(
                payload.len().saturating_sub(78),
                &children,
            )));
        }
        off += n;
        children.push(child);
    }
    Ok(BoxKind::Prefixed {
        prefix_len: 78,
        trailing,
        children,
    })
}

fn decode_wrap(parent: &[u8; 4], pos: u64, err: Mp4Error) -> Mp4Error {
    if err.message.starts_with("decode ") || err.message.starts_with("box extends") {
        return err;
    }
    Mp4Error::new(format!("decode {} pos {pos}: {err}", fourcc_str(parent)))
}

fn children_size_msg(parent_size: usize, children: &[IsoBox]) -> String {
    let mut msg = String::new();
    for child in children {
        msg.push_str(&format!(
            "{}:{} ",
            fourcc_str(&child.fourcc),
            child.raw.len()
        ));
    }
    format!("non-matching children box sizes, parentSize={parent_size}, {msg}")
}

enum Mode {
    Leaf,
    Kids,
    Stsd,
    Audio,
    Visual,
}

fn box_mode(parent: Option<[u8; 4]>, typ: [u8; 4]) -> Mode {
    if parent == Some(*b"stsd") {
        if typ == *b"enca" {
            return Mode::Audio;
        }
        if typ == *b"encv" {
            return Mode::Visual;
        }
        return Mode::Leaf;
    }
    if typ == *b"stsd" {
        return Mode::Stsd;
    }
    if is_container(&typ) {
        return Mode::Kids;
    }
    Mode::Leaf
}

fn is_container(typ: &[u8; 4]) -> bool {
    matches!(
        typ,
        b"moov"
            | b"trak"
            | b"mdia"
            | b"minf"
            | b"stbl"
            | b"mvex"
            | b"moof"
            | b"traf"
            | b"sinf"
            | b"schi"
    )
}

fn read_header(data: &[u8]) -> Result<(u64, usize, [u8; 4]), Mp4Error> {
    if data.len() < 8 {
        return Err(Mp4Error::new("unexpected EOF"));
    }
    let size32 = be_u32(data, 0)?;
    let mut fourcc = [0u8; 4];
    fourcc.copy_from_slice(&data[4..8]);
    if size32 == 0 {
        return Err(Mp4Error::new(
            "Size 0, meaning to end of file, not supported",
        ));
    }
    if size32 == 1 {
        if fourcc != *b"mdat" {
            return Err(Mp4Error::new(format!(
                "extended size not supported for box type {}",
                fourcc_str(&fourcc)
            )));
        }
        if data.len() < 16 {
            return Err(Mp4Error::new("unexpected EOF"));
        }
        let size = be_u64(data, 8)?;
        if size < 16 {
            return Err(Mp4Error::new(format!(
                "box header size 16 exceeds box size {size}"
            )));
        }
        return Ok((size, 16, fourcc));
    }
    let size = u64::from(size32);
    if size < 8 {
        return Err(Mp4Error::new(format!(
            "box header size 8 exceeds box size {size}"
        )));
    }
    Ok((size, 8, fourcc))
}

fn check_piff_senc(raw: &[u8], size: u64) -> Result<(), Mp4Error> {
    if raw.len() < 8 + 16 {
        return Ok(());
    }
    let uuid = &raw[8..24];
    if uuid != PIFF_SENC {
        return Ok(());
    }
    if size < 24 {
        return Err(Mp4Error::new(format!(
            "uuid box size too small: {size} < 24"
        )));
    }
    if raw.len() < 8 + 16 + 4 {
        return Err(Mp4Error::new(
            "failed to peek piff senc flags: unexpected EOF".to_string(),
        ));
    }
    if raw[8 + 16 + 3] & 0x1 != 0 {
        return Err(Mp4Error::new("piff senc override flag (0x1) not supported"));
    }
    Ok(())
}

fn be_u32(data: &[u8], at: usize) -> Result<u32, Mp4Error> {
    let s = data
        .get(at..at + 4)
        .ok_or_else(|| Mp4Error::new("unexpected EOF"))?;
    let mut buf = [0u8; 4];
    buf.copy_from_slice(s);
    Ok(u32::from_be_bytes(buf))
}

fn be_u64(data: &[u8], at: usize) -> Result<u64, Mp4Error> {
    let s = data
        .get(at..at + 8)
        .ok_or_else(|| Mp4Error::new("unexpected EOF"))?;
    let mut buf = [0u8; 8];
    buf.copy_from_slice(s);
    Ok(u64::from_be_bytes(buf))
}

fn be_i32(data: &[u8], at: usize) -> Result<i32, Mp4Error> {
    Ok(be_u32(data, at)? as i32)
}

fn fourcc_str(typ: &[u8; 4]) -> String {
    String::from_utf8_lossy(typ).into_owned()
}

fn stts_entry_count(moov: &IsoBox) -> Result<Option<u32>, Mp4Error> {
    let Some(trak) = find_child(moov, b"trak") else {
        return Ok(None);
    };
    let Some(mdia) = find_child(trak, b"mdia") else {
        return Ok(None);
    };
    let Some(minf) = find_child(mdia, b"minf") else {
        return Ok(None);
    };
    let Some(stbl) = find_child(minf, b"stbl") else {
        return Ok(None);
    };
    let Some(stts) = find_child(stbl, b"stts") else {
        return Ok(None);
    };
    let payload = payload_of(stts)?;
    if payload.len() < 8 {
        return Err(Mp4Error::new("stts payload is short"));
    }
    Ok(Some(be_u32(payload, 4)?))
}

fn payload_of(box_value: &IsoBox) -> Result<&[u8], Mp4Error> {
    box_value
        .raw
        .get(box_value.header_len..)
        .ok_or_else(|| Mp4Error::new("box payload is short"))
}

fn find_child<'a>(box_value: &'a IsoBox, typ: &[u8; 4]) -> Option<&'a IsoBox> {
    children_of(box_value).find(|child| child.fourcc == *typ)
}

fn children_of(box_value: &IsoBox) -> impl Iterator<Item = &IsoBox> {
    match &box_value.kind {
        BoxKind::Children(children) | BoxKind::Prefixed { children, .. } => {
            ChildrenIter::Full(children.iter())
        }
        BoxKind::Leaf => ChildrenIter::Empty,
    }
}

enum ChildrenIter<'a> {
    Empty,
    Full(std::slice::Iter<'a, IsoBox>),
}

impl<'a> Iterator for ChildrenIter<'a> {
    type Item = &'a IsoBox;
    fn next(&mut self) -> Option<Self::Item> {
        match self {
            ChildrenIter::Empty => None,
            ChildrenIter::Full(iter) => iter.next(),
        }
    }
}

fn children_mut(box_value: &mut IsoBox) -> Option<&mut Vec<IsoBox>> {
    match &mut box_value.kind {
        BoxKind::Children(children) | BoxKind::Prefixed { children, .. } => Some(children),
        BoxKind::Leaf => None,
    }
}

fn strip_init(init: &mut [IsoBox]) -> Result<Vec<TrackInfo>, Mp4Error> {
    let moov = init
        .iter_mut()
        .find(|b| b.fourcc == *b"moov")
        .ok_or_else(|| Mp4Error::new("no init segment file and no init part of file"))?;
    let mut tracks = Vec::new();
    let trak_count = children_of(moov).filter(|c| c.fourcc == *b"trak").count();
    for index in 0..trak_count {
        let trak_ptr = children_mut(moov)
            .and_then(|children| {
                children
                    .iter_mut()
                    .filter(|c| c.fourcc == *b"trak")
                    .nth(index)
            })
            .ok_or_else(|| Mp4Error::new("missing trak"))?;
        tracks.push(strip_trak(trak_ptr)?);
    }
    let trexes = read_trexes(moov)?;
    if !tracks.is_empty() && find_child(moov, b"mvex").is_none() {
        return Err(Mp4Error::new("missing mvex"));
    }
    for trex in trexes {
        for track in &mut tracks {
            if track.track_id == trex.track_id && track.trex.is_none() {
                track.trex = Some(trex.clone());
            }
        }
    }
    if let Some(children) = children_mut(moov) {
        for child in children.iter_mut() {
            if child.fourcc == *b"pssh" {
                child.drop = true;
            }
        }
    }
    Ok(tracks)
}

fn strip_trak(trak: &mut IsoBox) -> Result<TrackInfo, Mp4Error> {
    let track_id = read_track_id(trak)?;
    let stsd = trak_stsd_mut(trak)?;
    let mut scheme = String::new();
    let mut tenc = None;
    let entry_count = children_of(stsd).count();
    for index in 0..entry_count {
        let entry = children_mut(stsd)
            .and_then(|children| children.get_mut(index))
            .ok_or_else(|| Mp4Error::new("missing sample entry"))?;
        if entry.fourcc != *b"enca" && entry.fourcc != *b"encv" {
            continue;
        }
        let sinf = children_mut(entry)
            .and_then(|children| children.iter_mut().find(|c| c.fourcc == *b"sinf"))
            .ok_or_else(|| Mp4Error::new("does not have sinf box"))?;
        let parsed = read_sinf(sinf)?;
        scheme = parsed.0;
        tenc = Some(parsed.1);
        sinf.drop = true;
        let data_format = parsed.2;
        entry.rename = Some(data_format);
    }
    if !scheme.is_empty() && scheme != "cenc" && scheme != "cbcs" {
        return Err(Mp4Error::new(format!("scheme type {scheme} not supported")));
    }
    Ok(TrackInfo {
        track_id,
        scheme,
        tenc,
        trex: None,
    })
}

fn trak_stsd_mut(trak: &mut IsoBox) -> Result<&mut IsoBox, Mp4Error> {
    let mdia = children_mut(trak)
        .and_then(|c| c.iter_mut().find(|b| b.fourcc == *b"mdia"))
        .ok_or_else(|| Mp4Error::new("missing mdia"))?;
    let minf = children_mut(mdia)
        .and_then(|c| c.iter_mut().find(|b| b.fourcc == *b"minf"))
        .ok_or_else(|| Mp4Error::new("missing minf"))?;
    let stbl = children_mut(minf)
        .and_then(|c| c.iter_mut().find(|b| b.fourcc == *b"stbl"))
        .ok_or_else(|| Mp4Error::new("missing stbl"))?;
    children_mut(stbl)
        .and_then(|c| c.iter_mut().find(|b| b.fourcc == *b"stsd"))
        .ok_or_else(|| Mp4Error::new("missing stsd"))
}

fn read_track_id(trak: &IsoBox) -> Result<u32, Mp4Error> {
    let tkhd = find_child(trak, b"tkhd").ok_or_else(|| Mp4Error::new("missing tkhd"))?;
    let payload = payload_of(tkhd)?;
    if payload.is_empty() {
        return Err(Mp4Error::new("tkhd payload is short"));
    }
    let version = payload[0];
    let at = if version == 1 { 20 } else { 12 };
    be_u32(payload, at).map_err(|_| Mp4Error::new("tkhd payload is short"))
}

fn read_sinf(sinf: &IsoBox) -> Result<(String, TencInfo, [u8; 4]), Mp4Error> {
    let frma = find_child(sinf, b"frma").ok_or_else(|| Mp4Error::new("missing frma"))?;
    let frma_payload = payload_of(frma)?;
    if frma_payload.len() != 4 {
        return Err(Mp4Error::new("frma content length is not 4"));
    }
    let mut data_format = [0u8; 4];
    data_format.copy_from_slice(frma_payload);
    let schm = find_child(sinf, b"schm").ok_or_else(|| Mp4Error::new("missing schm"))?;
    let mut scheme = read_scheme(schm)?;
    let schi = find_child(sinf, b"schi").ok_or_else(|| Mp4Error::new("missing schi"))?;
    let mut tenc = read_tenc_box(schi)?;
    if scheme == "piff" {
        let algo = piff_algorithm(schi)?;
        scheme = match algo {
            1 => "cenc".to_string(),
            2 => "cbcs".to_string(),
            other => {
                return Err(Mp4Error::new(format!(
                    "piff unsupported algorithmID {other}"
                )))
            }
        };
        if tenc.is_none() {
            tenc = synthesize_piff_tenc(schi)?;
        }
    }
    let tenc = tenc.ok_or_else(|| Mp4Error::new("missing tenc"))?;
    Ok((scheme, tenc, data_format))
}

fn read_scheme(schm: &IsoBox) -> Result<String, Mp4Error> {
    let payload = payload_of(schm)?;
    if payload.len() < 8 {
        return Err(Mp4Error::new("schm payload is short"));
    }
    let mut scheme = [0u8; 4];
    scheme.copy_from_slice(&payload[4..8]);
    Ok(fourcc_str(&scheme))
}

fn read_tenc_box(schi: &IsoBox) -> Result<Option<TencInfo>, Mp4Error> {
    let Some(tenc) = find_child(schi, b"tenc") else {
        return Ok(None);
    };
    Ok(Some(parse_tenc_payload(payload_of(tenc)?)?))
}

fn parse_tenc_payload(payload: &[u8]) -> Result<TencInfo, Mp4Error> {
    if payload.len() < 24 {
        return Err(Mp4Error::new("tenc payload is short"));
    }
    let version = payload[0];
    let info = payload[5];
    let (crypt, skip) = if version == 0 {
        (0, 0)
    } else {
        (info >> 4, info & 0x0f)
    };
    let is_protected = payload[6];
    let iv_size = payload[7];
    let mut kid = [0u8; 16];
    kid.copy_from_slice(&payload[8..24]);
    let mut constant_iv = Vec::new();
    if is_protected == 1 && iv_size == 0 {
        if payload.len() < 25 {
            return Err(Mp4Error::new("tenc constant IV is short"));
        }
        let n = payload[24] as usize;
        let end = 25 + n;
        if payload.len() < end {
            return Err(Mp4Error::new("tenc constant IV is short"));
        }
        constant_iv = payload[25..end].to_vec();
    }
    Ok(TencInfo {
        crypt,
        skip,
        iv_size,
        kid,
        constant_iv,
    })
}

fn piff_algorithm(schi: &IsoBox) -> Result<u32, Mp4Error> {
    let Some(uuid) = find_piff_tenc(schi) else {
        if find_child(schi, b"tenc").is_none() {
            return Err(Mp4Error::new("piff scheme without piff tenc uuid"));
        }
        return Ok(0);
    };
    let payload = payload_of(uuid)?;
    if payload.len() < 16 + 4 + 3 {
        return Err(Mp4Error::new("piff tenc uuid box size too small"));
    }
    let algo_at = 16 + 4;
    let hi = payload[algo_at] as u32;
    let lo = u16::from_be_bytes([payload[algo_at + 1], payload[algo_at + 2]]) as u32;
    Ok((hi << 16) | lo)
}

fn synthesize_piff_tenc(schi: &IsoBox) -> Result<Option<TencInfo>, Mp4Error> {
    let Some(uuid) = find_piff_tenc(schi) else {
        return Ok(None);
    };
    let payload = payload_of(uuid)?;
    if payload.len() < 16 + 4 + 3 + 1 + 16 {
        return Err(Mp4Error::new(format!(
            "piff tenc uuid box size too small: {} < {}",
            uuid.raw.len(),
            8 + 16 + 24
        )));
    }
    let base = 16 + 4;
    let iv_size = payload[base + 3];
    let mut kid = [0u8; 16];
    kid.copy_from_slice(&payload[base + 4..base + 20]);
    Ok(Some(TencInfo {
        crypt: 0,
        skip: 0,
        iv_size,
        kid,
        constant_iv: Vec::new(),
    }))
}

fn find_piff_tenc(schi: &IsoBox) -> Option<&IsoBox> {
    children_of(schi).find(|child| {
        child.fourcc == *b"uuid" && child.raw.get(8..24).is_some_and(|uuid| uuid == PIFF_TENC)
    })
}

fn read_trexes(moov: &IsoBox) -> Result<Vec<TrexInfo>, Mp4Error> {
    let Some(mvex) = find_child(moov, b"mvex") else {
        return Ok(Vec::new());
    };
    let mut out = Vec::new();
    for child in children_of(mvex) {
        if child.fourcc != *b"trex" {
            continue;
        }
        let payload = payload_of(child)?;
        if payload.len() < 24 {
            return Err(Mp4Error::new("trex payload is short"));
        }
        out.push(TrexInfo {
            track_id: be_u32(payload, 4)?,
            default_size: be_u32(payload, 16)?,
        });
    }
    Ok(out)
}

fn parse_fragment_sencs(frag: &Frag, tracks: &[TrackInfo]) -> Result<(), Mp4Error> {
    let Some(moof) = frag.boxes.iter().find(|b| b.fourcc == *b"moof") else {
        return Ok(());
    };
    for traf in children_of(moof).filter(|c| c.fourcc == *b"traf") {
        if !traf_has_senc(traf) {
            continue;
        }
        let track = track_for_traf(traf, tracks)?;
        let Some(tenc) = track.and_then(|t| t.tenc.as_ref()) else {
            continue;
        };
        let _ = read_senc(traf, moof.start, tenc.iv_size)?;
    }
    Ok(())
}

fn track_for_traf<'a>(
    traf: &IsoBox,
    tracks: &'a [TrackInfo],
) -> Result<Option<&'a TrackInfo>, Mp4Error> {
    let Some(tfhd) = find_child(traf, b"tfhd") else {
        return Err(Mp4Error::new("missing tfhd"));
    };
    let info = parse_tfhd(tfhd)?;
    Ok(tracks.iter().find(|t| t.track_id == info.track_id))
}

fn traf_has_senc(traf: &IsoBox) -> bool {
    children_of(traf).any(|child| child.fourcc == *b"senc" || is_piff_senc(child))
}

fn is_piff_senc(box_value: &IsoBox) -> bool {
    box_value.fourcc == *b"uuid"
        && box_value
            .raw
            .get(8..24)
            .is_some_and(|uuid| uuid == PIFF_SENC)
}

fn read_senc(
    traf: &IsoBox,
    moof_start: u64,
    default_iv: u8,
) -> Result<Option<ParsedSenc>, Mp4Error> {
    let senc_box = children_of(traf).find(|c| c.fourcc == *b"senc");
    let uuid_box = children_of(traf).find(|c| is_piff_senc(c));
    let (start, payload, synthetic_header) = if let Some(senc) = senc_box {
        (senc.start, payload_of(senc)?.to_vec(), false)
    } else if let Some(uuid) = uuid_box {
        let payload = payload_of(uuid)?;
        if payload.len() < 16 + 8 {
            return Err(Mp4Error::new(format!(
                "uuid box size too small: {} < 24",
                uuid.raw.len()
            )));
        }
        if payload[16 + 3] & 0x1 != 0 {
            return Err(Mp4Error::new("piff senc override flag (0x1) not supported"));
        }
        (uuid.start + 16, payload[16..].to_vec(), true)
    } else {
        return Ok(None);
    };
    let _ = synthetic_header;
    if let Some(saio) = find_child(traf, b"saio") {
        let offset = first_saio_offset(saio)?;
        let pos = offset + moof_start as i64;
        let expected = start + 16;
        if pos < 0 || pos as u64 != expected {
            return Err(Mp4Error::new(format!(
                "offset from saio ({pos}) relative moof start differs from senc data start {expected}"
            )));
        }
    }
    let mut iv_size = default_iv;
    if let Some(seig_iv) = seig_iv_size(traf)? {
        iv_size = seig_iv;
    }
    let parsed = parse_senc_payload(&payload, iv_size, find_child(traf, b"saiz"))?;
    Ok(Some(ParsedSenc {
        ivs: parsed.0,
        subs: parsed.1,
    }))
}

fn first_saio_offset(saio: &IsoBox) -> Result<i64, Mp4Error> {
    let payload = payload_of(saio)?;
    if payload.len() < 8 {
        return Err(Mp4Error::new("saio payload is short"));
    }
    let flags = be_u32(payload, 0)? & 0x00ff_ffff;
    let version = payload[0];
    let mut at = 4usize;
    if flags & 0x1 != 0 {
        at += 8;
    }
    let count = be_u32(payload, at)? as usize;
    at += 4;
    if count == 0 {
        return Err(Mp4Error::new("saio missing offset"));
    }
    if version == 0 {
        Ok(i64::from(be_i32(payload, at)?))
    } else {
        Ok(be_u64(payload, at)? as i64)
    }
}

fn seig_iv_size(traf: &IsoBox) -> Result<Option<u8>, Mp4Error> {
    let sbgp = children_of(traf)
        .filter(|c| c.fourcc == *b"sbgp")
        .filter(|c| grouping_type(c).ok() == Some(*b"seig"))
        .last();
    let sgpd = children_of(traf)
        .filter(|c| c.fourcc == *b"sgpd")
        .filter(|c| grouping_type(c).ok() == Some(*b"seig"))
        .last();
    let (Some(sbgp), Some(sgpd)) = (sbgp, sgpd) else {
        return Ok(None);
    };
    let (counts, indices) = parse_sbgp(sbgp)?;
    if counts.len() != 1 {
        return Err(Mp4Error::new(format!(
            "sbgp entries = {}, only 1 supported for now",
            counts.len()
        )));
    }
    let index = indices.first().copied().unwrap_or(0);
    if index != 65537 {
        return Err(Mp4Error::new(
            "sgpd entry number must be first inside = 65536 + 1",
        ));
    }
    Ok(Some(first_seig_iv(sgpd)?))
}

fn grouping_type(box_value: &IsoBox) -> Result<[u8; 4], Mp4Error> {
    let payload = payload_of(box_value)?;
    if payload.len() < 8 {
        return Err(Mp4Error::new("grouping box is short"));
    }
    let mut typ = [0u8; 4];
    typ.copy_from_slice(&payload[4..8]);
    Ok(typ)
}

fn parse_sbgp(sbgp: &IsoBox) -> Result<(Vec<u32>, Vec<u32>), Mp4Error> {
    let payload = payload_of(sbgp)?;
    if payload.len() < 12 {
        return Err(Mp4Error::new("sbgp payload is short"));
    }
    let version = payload[0];
    let mut at = 8usize;
    if version == 1 {
        at += 4;
    }
    let count = be_u32(payload, at)? as usize;
    at += 4;
    let mut counts = Vec::new();
    let mut indices = Vec::new();
    for _ in 0..count {
        counts.push(be_u32(payload, at)?);
        indices.push(be_u32(payload, at + 4)?);
        at += 8;
    }
    Ok((counts, indices))
}

fn first_seig_iv(sgpd: &IsoBox) -> Result<u8, Mp4Error> {
    let payload = payload_of(sgpd)?;
    if payload.len() < 12 {
        return Err(Mp4Error::new("sgpd payload is short"));
    }
    let version = payload[0];
    let mut at = 8usize;
    let mut default_length = 0u32;
    if version >= 1 {
        default_length = be_u32(payload, at)?;
        at += 4;
    }
    if version >= 2 {
        at += 4;
    }
    let _count = be_u32(payload, at)?;
    at += 4;
    let mut length = default_length;
    if version >= 1 && default_length == 0 {
        length = be_u32(payload, at)?;
        at += 4;
    }
    if length == 0 || payload.len() < at + 4 {
        return Err(Mp4Error::new("sgpd: invalid descriptionLength of 0"));
    }
    // reserved, pattern, isProtected, perSampleIVSize
    Ok(payload[at + 3])
}

type IvList = Vec<Vec<u8>>;
type SubList = Vec<Vec<(u16, u32)>>;

fn parse_senc_payload(
    payload: &[u8],
    mut iv_size: u8,
    saiz: Option<&IsoBox>,
) -> Result<(IvList, SubList), Mp4Error> {
    if payload.len() < 8 {
        return Err(Mp4Error::new(format!(
            "payload size {} less than min size 8",
            payload.len()
        )));
    }
    let version = payload[0];
    if version > 0 {
        return Err(Mp4Error::new(format!("version {version} not supported")));
    }
    let flags = be_u32(payload, 0)? & 0x00ff_ffff;
    let sample_count = be_u32(payload, 4)?;
    let raw = &payload[8..];
    if flags & 0x2 != 0 && (payload.len() as u64 - 8) < 2 * u64::from(sample_count) {
        return Err(Mp4Error::new(format!(
            "payload size {} too small for {sample_count} samples and subSampleEncryption",
            payload.len()
        )));
    }
    if sample_count == 0 || raw.is_empty() {
        return Ok((Vec::new(), Vec::new()));
    }
    if flags & 0x2 == 0 {
        if iv_size == 0 {
            if sample_count == 0 {
                return Ok((Vec::new(), Vec::new()));
            }
            iv_size = (raw.len() as u32 / sample_count) as u8;
        }
        let ivs = match iv_size {
            0 => Vec::new(),
            8 | 16 => read_ivs(raw, sample_count, iv_size)?,
            _ => {
                return Err(Mp4Error::new(format!(
                    "strange derived PerSampleIVSize: {iv_size}"
                )))
            }
        };
        return Ok((ivs, Vec::new()));
    }
    if iv_size != 0 {
        return parse_subsamples(raw, sample_count, iv_size).ok_or_else(|| {
            Mp4Error::new(format!(
                "error decoding senc with perSampleIVSize = {iv_size}"
            ))
        });
    }
    let saiz_info = saiz.and_then(|box_value| parse_saiz(box_value).ok());
    for candidate in [0u8, 8, 16] {
        if let Some((default_size, sizes)) = &saiz_info {
            if !saiz_matches(*default_size, sizes, sample_count, candidate) {
                continue;
            }
        }
        if let Some(parsed) = parse_subsamples(raw, sample_count, candidate) {
            return Ok(parsed);
        }
    }
    Err(Mp4Error::new("could not decode senc"))
}

fn read_ivs(raw: &[u8], count: u32, iv_size: u8) -> Result<Vec<Vec<u8>>, Mp4Error> {
    let need = count as usize * iv_size as usize;
    if raw.len() < need {
        return Err(Mp4Error::new("senc IV payload is short"));
    }
    let mut ivs = Vec::new();
    for i in 0..count as usize {
        let start = i * iv_size as usize;
        ivs.push(raw[start..start + iv_size as usize].to_vec());
    }
    Ok(ivs)
}

fn parse_subsamples(raw: &[u8], count: u32, iv_size: u8) -> Option<(IvList, SubList)> {
    let mut at = 0usize;
    let mut ivs = Vec::new();
    let mut subs = Vec::new();
    for _ in 0..count {
        if iv_size > 0 {
            let n = iv_size as usize;
            if raw.len() < at + n {
                return None;
            }
            ivs.push(raw[at..at + n].to_vec());
            at += n;
        }
        if raw.len() < at + 2 {
            return None;
        }
        let nsub = u16::from_be_bytes([raw[at], raw[at + 1]]) as usize;
        at += 2;
        if raw.len() < at + nsub * 6 {
            return None;
        }
        let mut sample_subs = Vec::new();
        for _ in 0..nsub {
            let clear = u16::from_be_bytes([raw[at], raw[at + 1]]);
            let protected =
                u32::from_be_bytes([raw[at + 2], raw[at + 3], raw[at + 4], raw[at + 5]]);
            sample_subs.push((clear, protected));
            at += 6;
        }
        subs.push(sample_subs);
    }
    if at != raw.len() {
        return None;
    }
    Some((ivs, subs))
}

fn parse_saiz(saiz: &IsoBox) -> Result<(u8, Vec<u8>), Mp4Error> {
    let payload = payload_of(saiz)?;
    if payload.len() < 9 {
        return Err(Mp4Error::new("saiz payload is short"));
    }
    let flags = be_u32(payload, 0)? & 0x00ff_ffff;
    let mut at = 4usize;
    if flags & 1 != 0 {
        at += 8;
    }
    let default_size = *payload
        .get(at)
        .ok_or_else(|| Mp4Error::new("saiz payload is short"))?;
    at += 1;
    let count = be_u32(payload, at)? as usize;
    at += 4;
    let mut sizes = Vec::new();
    if default_size == 0 {
        if payload.len() < at + count {
            return Err(Mp4Error::new("saiz payload is short"));
        }
        sizes.extend_from_slice(&payload[at..at + count]);
    }
    Ok((default_size, sizes))
}

fn saiz_matches(default_size: u8, sizes: &[u8], sample_count: u32, iv_size: u8) -> bool {
    for i in 0..sample_count {
        let mut sample_info = default_size;
        if sample_info == 0 {
            let Some(value) = sizes.get(i as usize) else {
                return false;
            };
            sample_info = *value;
        }
        if sample_info == 0 {
            continue;
        }
        let remaining = sample_info as i32 - iv_size as i32;
        if remaining < 2 || (remaining - 2) % 6 != 0 {
            return false;
        }
    }
    true
}

fn decrypt_segment(
    seg: &mut Seg,
    tracks: &[TrackInfo],
    keys: &KeyMaterial,
) -> Result<(), Mp4Error> {
    let mut decrypted = 0usize;
    for frag in &mut seg.frags {
        if !fragment_has_senc(frag) {
            continue;
        }
        decrypt_fragment(frag, tracks, keys)
            .map_err(|err| Mp4Error::new(format!("decryptSegment: {err}")))?;
        decrypted += 1;
    }
    if decrypted == 0 && !seg.frags.is_empty() {
        return Err(Mp4Error::new(
            "decryptSegment: encrypted MP4 fragment had no senc box",
        ));
    }
    Ok(())
}

fn fragment_has_senc(frag: &Frag) -> bool {
    frag.boxes.iter().any(|b| {
        b.fourcc == *b"moof"
            && children_of(b)
                .filter(|c| c.fourcc == *b"traf")
                .any(traf_has_senc)
    })
}

fn decrypt_fragment(
    frag: &mut Frag,
    tracks: &[TrackInfo],
    keys: &KeyMaterial,
) -> Result<(), Mp4Error> {
    let moof_i = frag
        .boxes
        .iter()
        .position(|b| b.fourcc == *b"moof")
        .ok_or_else(|| Mp4Error::new("moof not set in fragment"))?;
    let mdat_i = frag
        .boxes
        .iter()
        .position(|b| b.fourcc == *b"mdat")
        .ok_or_else(|| Mp4Error::new("mdat not set in fragment"))?;
    let (moof, mdat) = two_mut(&mut frag.boxes, moof_i, mdat_i)?;
    let moof_start = moof.start;
    let mdat_header = mdat.header_len;
    let mdat_start = mdat.start;
    let traf_count = children_of(moof).filter(|c| c.fourcc == *b"traf").count();
    let mut removed = 0u64;
    for index in 0..traf_count {
        let traf = children_mut(moof)
            .and_then(|c| c.iter_mut().filter(|b| b.fourcc == *b"traf").nth(index))
            .ok_or_else(|| Mp4Error::new("missing traf"))?;
        let tfhd = find_child(traf, b"tfhd").ok_or_else(|| Mp4Error::new("missing tfhd"))?;
        let tfhd = parse_tfhd(tfhd)?;
        let Some(track) = tracks.iter().find(|t| t.track_id == tfhd.track_id) else {
            continue;
        };
        let Some(tenc) = track.tenc.as_ref() else {
            continue;
        };
        if track.scheme != "cenc" && track.scheme != "cbcs" {
            return Err(Mp4Error::new(format!(
                "scheme type {} not supported",
                track.scheme
            )));
        }
        let has_senc = traf_has_senc(traf);
        if !has_senc && (tenc.iv_size != 0 || tenc.constant_iv.is_empty()) {
            return Err(Mp4Error::new("no senc box in traf"));
        }
        let senc = if has_senc {
            read_senc(traf, moof_start, tenc.iv_size)?
        } else {
            None
        };
        let key = track_key(track, keys)?;
        let samples = sample_ranges(
            traf,
            &tfhd,
            track.trex.as_ref(),
            moof_start,
            mdat_start,
            mdat_header,
            payload_len(mdat),
        )?;
        let payload = mdat_payload_mut(mdat)?;
        decrypt_samples(&track.scheme, payload, &samples, &key, tenc, senc.as_ref())?;
        removed += mark_encryption_boxes(traf);
    }
    if let Some(children) = children_mut(moof) {
        for child in children.iter_mut() {
            if child.fourcc == *b"pssh" {
                removed += child.raw.len() as u64;
                child.drop = true;
            }
        }
    }
    propagate(moof);
    propagate(mdat);
    let moof_size = moof.encoded_len();
    patch_truns(moof, moof_size, mdat_header as u64, removed)?;
    propagate(moof);
    Ok(())
}

fn two_mut(
    boxes: &mut [IsoBox],
    a: usize,
    b: usize,
) -> Result<(&mut IsoBox, &mut IsoBox), Mp4Error> {
    if a == b {
        return Err(Mp4Error::new("moof and mdat are the same box"));
    }
    if a < b {
        let (left, right) = boxes.split_at_mut(b);
        Ok((&mut left[a], &mut right[0]))
    } else {
        let (left, right) = boxes.split_at_mut(a);
        Ok((&mut right[0], &mut left[b]))
    }
}

fn payload_len(mdat: &IsoBox) -> usize {
    mdat.raw.len().saturating_sub(mdat.header_len)
}

fn mdat_payload_mut(mdat: &mut IsoBox) -> Result<&mut Vec<u8>, Mp4Error> {
    if mdat.payload_override.is_none() {
        let payload = mdat
            .raw
            .get(mdat.header_len..)
            .ok_or_else(|| Mp4Error::new("mdat payload is short"))?
            .to_vec();
        mdat.payload_override = Some(payload);
        mdat.dirty = true;
    }
    mdat.payload_override
        .as_mut()
        .ok_or_else(|| Mp4Error::new("missing mdat payload"))
}

fn mark_encryption_boxes(traf: &mut IsoBox) -> u64 {
    let mut removed = 0u64;
    let Some(children) = children_mut(traf) else {
        return 0;
    };
    for child in children.iter_mut() {
        let drop_box = match &child.fourcc {
            b"saiz" | b"saio" | b"senc" => true,
            b"uuid" if is_piff_senc(child) => true,
            b"sbgp" | b"sgpd" => match grouping_type(child) {
                Ok(typ) => typ == *b"seig" || typ == *b"seam",
                Err(_) => false,
            },
            _ => false,
        };
        if drop_box {
            removed += child.raw.len() as u64;
            child.drop = true;
        }
    }
    removed
}

fn track_key(track: &TrackInfo, keys: &KeyMaterial) -> Result<Vec<u8>, Mp4Error> {
    if keys.by_kid.is_empty() {
        return keys
            .legacy
            .clone()
            .ok_or_else(|| Mp4Error::new("no mp4ff key specs provided"));
    }
    let Some(tenc) = &track.tenc else {
        return Err(Mp4Error::new(format!(
            "missing tenc for trackID={}",
            track.track_id
        )));
    };
    if tenc.kid.len() != 16 {
        return Err(Mp4Error::new(format!(
            "bad kid length {} for trackID={}",
            tenc.kid.len(),
            track.track_id
        )));
    }
    let kid_hex = hex::encode(tenc.kid);
    if let Some(key) = keys.by_kid.get(&kid_hex) {
        return Ok(key.clone());
    }
    if keys.strict {
        return Err(Mp4Error::new(format!(
            "requested key was not found for kid={kid_hex}"
        )));
    }
    keys.legacy
        .clone()
        .ok_or_else(|| Mp4Error::new(format!("requested key was not found for kid={kid_hex}")))
}

fn parse_tfhd(tfhd: &IsoBox) -> Result<TfhdInfo, Mp4Error> {
    let payload = payload_of(tfhd)?;
    if payload.len() < 8 {
        return Err(Mp4Error::new("tfhd payload is short"));
    }
    let flags = be_u32(payload, 0)? & 0x00ff_ffff;
    let mut at = 8usize;
    let mut info = TfhdInfo {
        flags,
        track_id: be_u32(payload, 4)?,
        base_data_offset: 0,
        default_duration: 0,
        default_size: 0,
        default_flags: 0,
    };
    if flags & 0x1 != 0 {
        info.base_data_offset = be_u64(payload, at)?;
        at += 8;
    }
    if flags & 0x2 != 0 {
        at += 4;
    }
    if flags & 0x8 != 0 {
        info.default_duration = be_u32(payload, at)?;
        at += 4;
    }
    if flags & 0x10 != 0 {
        info.default_size = be_u32(payload, at)?;
        at += 4;
    }
    if flags & 0x20 != 0 {
        info.default_flags = be_u32(payload, at)?;
    }
    Ok(info)
}

fn sample_ranges(
    traf: &IsoBox,
    tfhd: &TfhdInfo,
    trex: Option<&TrexInfo>,
    moof_start: u64,
    mdat_start: u64,
    mdat_header: usize,
    mdat_len: usize,
) -> Result<Vec<(usize, usize)>, Mp4Error> {
    let mut ranges = Vec::new();
    for trun_box in children_of(traf).filter(|c| c.fourcc == *b"trun") {
        let trun = parse_trun(trun_box, tfhd, trex)?;
        let mut base = moof_start as i64;
        if tfhd.flags & 0x1 != 0 {
            base = tfhd.base_data_offset as i64;
        } else if tfhd.flags & 0x020000 != 0 {
            base = moof_start as i64;
        }
        if let Some(offset) = trun.data_offset {
            base += i64::from(offset);
        }
        let payload_abs = mdat_start as i64 + mdat_header as i64;
        let offset_in_mdat = if base > 0 { base - payload_abs } else { 0 };
        if offset_in_mdat < 0 || offset_in_mdat as u64 > mdat_len as u64 {
            return Err(Mp4Error::new("offset in mdata beyond size"));
        }
        let mut cursor = offset_in_mdat as usize;
        for sample in &trun.samples {
            let size = sample.size as usize;
            if cursor.checked_add(size).is_none() || cursor + size > mdat_len {
                return Err(Mp4Error::new("sample extends past mdat"));
            }
            ranges.push((cursor, size));
            cursor += size;
        }
    }
    Ok(ranges)
}

fn parse_trun(
    trun: &IsoBox,
    tfhd: &TfhdInfo,
    trex: Option<&TrexInfo>,
) -> Result<TrunInfo, Mp4Error> {
    let payload = payload_of(trun)?;
    if payload.len() < 8 {
        return Err(Mp4Error::new("trun payload is short"));
    }
    let flags = be_u32(payload, 0)? & 0x00ff_ffff;
    let count = be_u32(payload, 4)?;
    let stride = flag_bytes(flags & 0x100)
        + flag_bytes(flags & 0x200)
        + flag_bytes(flags & 0x400)
        + flag_bytes(flags & 0x800);
    if count > 1024 && stride == 0 {
        return Err(Mp4Error::new(format!(
            "trun: sampleCount {count} is big but no sample data present"
        )));
    }
    let mut at = 8usize;
    let data_offset = if flags & 0x1 != 0 {
        let value = be_i32(payload, at)?;
        at += 4;
        Some(value)
    } else {
        None
    };
    if flags & 0x4 != 0 {
        at += 4;
    }
    let need = at + count as usize * stride;
    if payload.len() < need {
        return Err(Mp4Error::new(format!(
            "trun: expected size {}, got {}",
            trun.header_len + need,
            trun.raw.len()
        )));
    }
    let default_size = if tfhd.flags & 0x10 != 0 {
        tfhd.default_size
    } else {
        trex.map(|t| t.default_size).unwrap_or(0)
    };
    let mut samples = Vec::with_capacity(count as usize);
    for _ in 0..count {
        let mut size = default_size;
        if flags & 0x100 != 0 {
            at += 4;
        }
        if flags & 0x200 != 0 {
            size = be_u32(payload, at)?;
            at += 4;
        }
        if flags & 0x400 != 0 {
            at += 4;
        }
        if flags & 0x800 != 0 {
            at += 4;
        }
        samples.push(TrunSample { size });
    }
    Ok(TrunInfo {
        data_offset,
        samples,
    })
}

fn flag_bytes(flag: u32) -> usize {
    if flag == 0 {
        0
    } else {
        4
    }
}

fn decrypt_samples(
    scheme: &str,
    payload: &mut [u8],
    samples: &[(usize, usize)],
    key: &[u8],
    tenc: &TencInfo,
    senc: Option<&ParsedSenc>,
) -> Result<(), Mp4Error> {
    if key.len() != 16 && key.len() != 24 && key.len() != 32 {
        return Err(Mp4Error::new(format!(
            "crypto/aes: invalid key size {}",
            key.len()
        )));
    }
    let mut iv = [0u8; 16];
    if !tenc.constant_iv.is_empty() {
        let n = tenc.constant_iv.len().min(16);
        iv[..n].copy_from_slice(&tenc.constant_iv[..n]);
    }
    for (index, (offset, size)) in samples.iter().copied().enumerate() {
        let sample = payload
            .get_mut(offset..offset + size)
            .ok_or_else(|| Mp4Error::new("sample extends past mdat"))?;
        if let Some(senc) = senc {
            if senc.ivs.len() == samples.len() {
                let sample_iv = senc
                    .ivs
                    .get(index)
                    .ok_or_else(|| Mp4Error::new("senc IV count does not match samples"))?;
                if sample_iv.len() < 16 {
                    iv = [0u8; 16];
                }
                let n = sample_iv.len().min(16);
                iv[..n].copy_from_slice(&sample_iv[..n]);
            }
        }
        let pattern = senc.and_then(|s| {
            if s.subs.is_empty() {
                None
            } else {
                s.subs.get(index)
            }
        });
        match scheme {
            "cenc" => crypt_cenc(sample, key, &iv, pattern)?,
            "cbcs" => crypt_cbcs(sample, key, &iv, pattern, tenc)?,
            other => return Err(Mp4Error::new(format!("scheme type {other} not supported"))),
        }
    }
    Ok(())
}

fn crypt_cenc(
    sample: &mut [u8],
    key: &[u8],
    iv: &[u8; 16],
    pattern: Option<&Vec<(u16, u32)>>,
) -> Result<(), Mp4Error> {
    let cipher = aes_cipher(key)?;
    let mut counter = *iv;
    let Some(subs) = pattern else {
        ctr_xor(&cipher, &mut counter, sample);
        return Ok(());
    };
    if subs.is_empty() {
        ctr_xor(&cipher, &mut counter, sample);
        return Ok(());
    }
    let mut pos = 0usize;
    for (clear, protected) in subs {
        pos = pos.saturating_add(*clear as usize);
        let n = *protected as usize;
        if pos.checked_add(n).is_none() || pos + n > sample.len() {
            return Err(Mp4Error::new("subsample exceeds sample"));
        }
        if n > 0 {
            ctr_xor(&cipher, &mut counter, &mut sample[pos..pos + n]);
            pos += n;
        }
    }
    Ok(())
}

fn crypt_cbcs(
    sample: &mut [u8],
    key: &[u8],
    iv: &[u8; 16],
    pattern: Option<&Vec<(u16, u32)>>,
    tenc: &TencInfo,
) -> Result<(), Mp4Error> {
    let crypt_bytes = tenc.crypt as usize * 16;
    let skip_bytes = tenc.skip as usize * 16;
    let Some(subs) = pattern else {
        return cbcs_range(sample, key, iv, crypt_bytes, skip_bytes);
    };
    if subs.is_empty() {
        return cbcs_range(sample, key, iv, crypt_bytes, skip_bytes);
    }
    let mut pos = 0usize;
    for (clear, protected) in subs {
        pos = pos.saturating_add(*clear as usize);
        let n = *protected as usize;
        if pos.checked_add(n).is_none() || pos + n > sample.len() {
            return Err(Mp4Error::new("subsample exceeds sample"));
        }
        if n > 0 {
            cbcs_range(&mut sample[pos..pos + n], key, iv, crypt_bytes, skip_bytes)?;
            pos += n;
        }
    }
    Ok(())
}

fn cbcs_range(
    data: &mut [u8],
    key: &[u8],
    iv: &[u8; 16],
    crypt_bytes: usize,
    skip_bytes: usize,
) -> Result<(), Mp4Error> {
    let cipher = aes_cipher(key)?;
    if skip_bytes == 0 {
        let n = data.len() & !0x0f;
        cbc_decrypt(&cipher, iv, &mut data[..n])?;
        return Ok(());
    }
    if crypt_bytes == 0 || !crypt_bytes.is_multiple_of(16) {
        return Err(Mp4Error::new("cbcs crypt pattern is empty"));
    }
    let mut prev = *iv;
    let mut pos = 0usize;
    while data.len() - pos >= crypt_bytes {
        cbc_decrypt_from(&cipher, &mut prev, &mut data[pos..pos + crypt_bytes])?;
        pos += crypt_bytes;
        if data.len() - pos < skip_bytes {
            break;
        }
        pos += skip_bytes;
    }
    Ok(())
}

fn cbc_decrypt(cipher: &Aes128, iv: &[u8; 16], data: &mut [u8]) -> Result<(), Mp4Error> {
    let mut prev = *iv;
    cbc_decrypt_from(cipher, &mut prev, data)
}

fn cbc_decrypt_from(cipher: &Aes128, prev: &mut [u8; 16], data: &mut [u8]) -> Result<(), Mp4Error> {
    if !data.len().is_multiple_of(16) {
        return Err(Mp4Error::new("cbcs range is not a multiple of 16"));
    }
    for chunk in data.chunks_mut(16) {
        let mut cipher_block = [0u8; 16];
        cipher_block.copy_from_slice(chunk);
        let mut block = aes::cipher::generic_array::GenericArray::clone_from_slice(&cipher_block);
        cipher.decrypt_block(&mut block);
        for i in 0..16 {
            chunk[i] = block[i] ^ prev[i];
        }
        *prev = cipher_block;
    }
    Ok(())
}

fn ctr_xor(cipher: &Aes128, counter: &mut [u8; 16], data: &mut [u8]) {
    let mut offset = 0usize;
    while offset < data.len() {
        let mut block = aes::cipher::generic_array::GenericArray::clone_from_slice(counter);
        cipher.encrypt_block(&mut block);
        let n = (data.len() - offset).min(16);
        for i in 0..n {
            data[offset + i] ^= block[i];
        }
        offset += n;
        increment_counter(counter);
    }
}

fn increment_counter(counter: &mut [u8; 16]) {
    for byte in counter.iter_mut().rev() {
        let (next, overflow) = byte.overflowing_add(1);
        *byte = next;
        if !overflow {
            break;
        }
    }
}

fn aes_cipher(key: &[u8]) -> Result<Aes128, Mp4Error> {
    if key.len() != 16 {
        return Err(Mp4Error::new(format!(
            "crypto/aes: invalid key size {}",
            key.len()
        )));
    }
    Aes128::new_from_slice(key).map_err(|err| Mp4Error::new(err.to_string()))
}

fn patch_truns(
    moof: &mut IsoBox,
    moof_size: u64,
    mdat_header: u64,
    removed: u64,
) -> Result<(), Mp4Error> {
    let trun_count = count_truns(moof);
    if trun_count == 0 {
        return Ok(());
    }
    let recompute = trun_count == 1;
    let traf_count = children_of(moof).filter(|c| c.fourcc == *b"traf").count();
    let mut data_offset = moof_size + mdat_header;
    for index in 0..traf_count {
        let traf = children_mut(moof)
            .and_then(|c| c.iter_mut().filter(|b| b.fourcc == *b"traf").nth(index))
            .ok_or_else(|| Mp4Error::new("missing traf"))?;
        let Some(children) = children_mut(traf) else {
            continue;
        };
        for trun in children.iter_mut().filter(|c| c.fourcc == *b"trun") {
            let payload = payload_of(trun)?;
            if payload.len() < 8 {
                return Err(Mp4Error::new("trun payload is short"));
            }
            let flags = be_u32(payload, 0)? & 0x00ff_ffff;
            if flags & 0x1 == 0 {
                continue;
            }
            let current = be_i32(payload, 8)?;
            let new_offset = if recompute {
                i32::try_from(data_offset)
                    .map_err(|_| Mp4Error::new("trun data offset overflows"))?
            } else {
                current - i32::try_from(removed).unwrap_or(i32::MAX)
            };
            let mut next = payload.to_vec();
            next[8..12].copy_from_slice(&new_offset.to_be_bytes());
            trun.payload_override = Some(next);
            trun.dirty = true;
            if recompute {
                let info = parse_trun(trun, &empty_tfhd(), None)?;
                let data_size: u64 = info.samples.iter().map(|s| u64::from(s.size)).sum();
                data_offset += data_size;
            }
        }
    }
    Ok(())
}

fn empty_tfhd() -> TfhdInfo {
    TfhdInfo {
        flags: 0,
        track_id: 0,
        base_data_offset: 0,
        default_duration: 0,
        default_size: 0,
        default_flags: 0,
    }
}

fn count_truns(moof: &IsoBox) -> usize {
    children_of(moof)
        .filter(|c| c.fourcc == *b"traf")
        .map(|traf| children_of(traf).filter(|c| c.fourcc == *b"trun").count())
        .sum()
}

fn propagate(box_value: &mut IsoBox) {
    let mut changed = box_value.rename.is_some() || box_value.payload_override.is_some();
    match &mut box_value.kind {
        BoxKind::Leaf => {}
        BoxKind::Children(children) | BoxKind::Prefixed { children, .. } => {
            for child in children.iter_mut() {
                propagate(child);
                if child.dirty
                    || child.drop
                    || child.rename.is_some()
                    || child.payload_override.is_some()
                {
                    changed = true;
                }
            }
        }
    }
    if changed {
        box_value.dirty = true;
    }
}

fn encode_doc(doc: &mut Mp4Doc) -> Result<Vec<u8>, Mp4Error> {
    for b in &mut doc.init {
        propagate(b);
    }
    for seg in &mut doc.segments {
        if let Some(styp) = &mut seg.styp {
            propagate(styp);
        }
        for frag in &mut seg.frags {
            for b in &mut frag.boxes {
                propagate(b);
            }
        }
    }
    let mut out = Vec::new();
    for b in &doc.init {
        emit(b, &mut out)?;
    }
    for seg in &doc.segments {
        if let Some(styp) = &seg.styp {
            emit(styp, &mut out)?;
        }
        for frag in &seg.frags {
            for b in &frag.boxes {
                emit(b, &mut out)?;
            }
        }
    }
    Ok(out)
}

fn emit(box_value: &IsoBox, out: &mut Vec<u8>) -> Result<(), Mp4Error> {
    if box_value.drop {
        return Ok(());
    }
    if !needs_rebuild(box_value) {
        out.extend_from_slice(&box_value.raw);
        return Ok(());
    }
    let fourcc = box_value.rename.unwrap_or(box_value.fourcc);
    match &box_value.kind {
        BoxKind::Leaf => {
            let payload = if let Some(p) = &box_value.payload_override {
                p.as_slice()
            } else {
                box_value
                    .raw
                    .get(box_value.header_len..)
                    .ok_or_else(|| Mp4Error::new("box payload is short"))?
            };
            write_header(out, fourcc, box_value.header_len, payload.len())?;
            out.extend_from_slice(payload);
        }
        BoxKind::Children(children) => {
            let start = out.len();
            out.extend_from_slice(&[0, 0, 0, 0]);
            out.extend_from_slice(&fourcc);
            for child in children {
                emit(child, out)?;
            }
            patch_size(out, start)?;
        }
        BoxKind::Prefixed {
            prefix_len,
            trailing,
            children,
        } => {
            let payload = box_value
                .raw
                .get(box_value.header_len..)
                .ok_or_else(|| Mp4Error::new("box payload is short"))?;
            if payload.len() < *prefix_len {
                return Err(Mp4Error::new("prefixed box is short"));
            }
            let start = out.len();
            out.extend_from_slice(&[0, 0, 0, 0]);
            out.extend_from_slice(&fourcc);
            out.extend_from_slice(&payload[..*prefix_len]);
            for child in children {
                emit(child, out)?;
            }
            out.extend_from_slice(trailing);
            patch_size(out, start)?;
        }
    }
    Ok(())
}

fn needs_rebuild(box_value: &IsoBox) -> bool {
    box_value.dirty || box_value.rename.is_some() || box_value.payload_override.is_some()
}

impl IsoBox {
    fn encoded_len(&self) -> u64 {
        if self.drop {
            return 0;
        }
        if !needs_rebuild(self) {
            return self.raw.len() as u64;
        }
        match &self.kind {
            BoxKind::Leaf => {
                let n = self
                    .payload_override
                    .as_ref()
                    .map(|p| p.len())
                    .unwrap_or_else(|| self.raw.len().saturating_sub(self.header_len));
                (self.header_len + n) as u64
            }
            BoxKind::Children(children) => {
                8 + children.iter().map(IsoBox::encoded_len).sum::<u64>()
            }
            BoxKind::Prefixed {
                prefix_len,
                trailing,
                children,
            } => {
                8 + *prefix_len as u64
                    + trailing.len() as u64
                    + children.iter().map(IsoBox::encoded_len).sum::<u64>()
            }
        }
    }
}

fn write_header(
    out: &mut Vec<u8>,
    fourcc: [u8; 4],
    header_len: usize,
    payload_len: usize,
) -> Result<(), Mp4Error> {
    let size = header_len + payload_len;
    if header_len == 16 {
        let size_u = u64::try_from(size).map_err(|_| Mp4Error::new("box size overflows"))?;
        out.extend_from_slice(&1u32.to_be_bytes());
        out.extend_from_slice(&fourcc);
        out.extend_from_slice(&size_u.to_be_bytes());
        return Ok(());
    }
    let size_u = u32::try_from(size).map_err(|_| Mp4Error::new("box size overflows"))?;
    out.extend_from_slice(&size_u.to_be_bytes());
    out.extend_from_slice(&fourcc);
    Ok(())
}

fn patch_size(out: &mut [u8], start: usize) -> Result<(), Mp4Error> {
    let size = out.len() - start;
    let size_u = u32::try_from(size).map_err(|_| Mp4Error::new("box size overflows"))?;
    out.get_mut(start..start + 4)
        .ok_or_else(|| Mp4Error::new("box size patch is out of range"))?
        .copy_from_slice(&size_u.to_be_bytes());
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture(name: &str) -> std::path::PathBuf {
        Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("tests/fixtures/amazon")
            .join(name)
    }

    fn key() -> String {
        "00112233445566778899aabbccddeeff".to_string()
    }

    fn kid_key() -> String {
        "11112222333344445555666677778888:00112233445566778899aabbccddeeff".to_string()
    }

    fn assert_decrypt(enc: &str, dec: &str, spec: &str) {
        let dir = tempfile::tempdir().expect("tempdir");
        let out = dir.path().join("out.mp4");
        decrypt_with_mp4ff(&[spec.to_string()], &fixture(enc), &out).unwrap_or_else(|err| {
            panic!("{enc} decrypt failed: {err}");
        });
        let got = std::fs::read(&out).expect("read out");
        let want = std::fs::read(fixture(dec)).expect("read dec");
        if got != want {
            let at = got.iter().zip(want.iter()).position(|(a, b)| a != b);
            panic!(
                "{enc} bytes differ: got {} want {} first {at:?}",
                got.len(),
                want.len()
            );
        }
    }

    #[test]
    fn test_decrypt_cenc16_matches_go() {
        assert_decrypt("cenc16.enc.mp4", "cenc16.dec.mp4", &key());
    }

    #[test]
    fn test_decrypt_cenc8_matches_go() {
        assert_decrypt("cenc8.enc.mp4", "cenc8.dec.mp4", &key());
    }

    #[test]
    fn test_decrypt_cenc_sub_matches_go() {
        assert_decrypt("cenc_sub.enc.mp4", "cenc_sub.dec.mp4", &kid_key());
    }

    #[test]
    fn test_decrypt_cbcs_sub_matches_go() {
        assert_decrypt("cbcs_sub.enc.mp4", "cbcs_sub.dec.mp4", &kid_key());
    }

    #[test]
    fn test_decrypt_cbcs_nosenc_errors() {
        let dir = tempfile::tempdir().expect("tempdir");
        let out = dir.path().join("out.mp4");
        let err = decrypt_with_mp4ff(&[key()], &fixture("cbcs_nosenc.enc.mp4"), &out)
            .expect_err("nosenc");
        assert_eq!(
            err.to_string(),
            "mp4ff decryption failed: decryptSegment: encrypted MP4 fragment had no senc box"
        );
        assert!(!out.exists());
    }

    #[test]
    fn test_decrypt_missing_input_does_not_create_output() {
        let dir = tempfile::tempdir().expect("tempdir");
        let missing = dir.path().join("missing.mp4");
        let out = dir.path().join("out.mp4");
        let err = decrypt_with_mp4ff(&[key()], &missing, &out).expect_err("missing");
        assert!(err.to_string().starts_with("failed to open encrypted MP4:"));
        assert!(!out.exists());
    }

    #[test]
    fn test_decrypt_wrong_kid() {
        let dir = tempfile::tempdir().expect("tempdir");
        let out = dir.path().join("out.mp4");
        let spec = "00000000000000000000000000000000:00112233445566778899aabbccddeeff";
        let err = decrypt_with_mp4ff(&[spec.to_string()], &fixture("cenc16.enc.mp4"), &out)
            .expect_err("kid");
        assert_eq!(
            err.to_string(),
            "mp4ff decryption failed: decryptSegment: requested key was not found for kid=11112222333344445555666677778888"
        );
        assert!(!out.exists());
    }

    #[test]
    fn test_parse_mp4ff_key_specs() {
        let err = parse_mp4ff_key_specs(&[]).expect_err("empty");
        assert_eq!(err.to_string(), "no mp4ff key specs provided");

        let err = parse_mp4ff_key_specs(&[String::new(), "   ".to_string()]).expect_err("blank");
        assert_eq!(err.to_string(), "no mp4ff key specs provided");

        let err = parse_mp4ff_key_specs(&[key(), "22222222222222222222222222222222".to_string()])
            .expect_err("multi");
        assert_eq!(err.to_string(), "multiple legacy keys are not supported");

        let err = parse_mp4ff_key_specs(&[key(), kid_key()]).expect_err("mix");
        assert_eq!(
            err.to_string(),
            "cannot mix legacy key and kid:key key format"
        );

        let err =
            parse_mp4ff_key_specs(&["not-a-kid:00112233445566778899aabbccddeeff".to_string()])
                .expect_err("kid");
        assert_eq!(
            err.to_string(),
            "bad kid in key spec \"not-a-kid:00112233445566778899aabbccddeeff\""
        );

        let parsed = parse_mp4ff_key_specs(&["1:00112233445566778899aabbccddeeff".to_string()])
            .expect("decimal");
        assert_eq!(parsed.legacy.unwrap(), hex_decode(&key()).unwrap());

        let err = parse_mp4ff_key_specs(&[
            kid_key(),
            "11112222333344445555666677778888:00112233445566778899aabbccddeeff".to_string(),
        ]);
        assert!(err.is_ok(), "duplicate normalized spec is skipped");

        let err = parse_mp4ff_key_specs(&[
            kid_key(),
            "11112222333344445555666677778888:ffeeddccbbaa99887766554433221100".to_string(),
        ])
        .expect_err("dup kid");
        assert_eq!(
            err.to_string(),
            "duplicate kid 11112222333344445555666677778888"
        );

        let err = parse_mp4ff_key_specs(&["zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz".to_string()])
            .expect_err("hex");
        assert!(err
            .to_string()
            .starts_with("unpacking key: bad hex zzzzzz...zzzzzz:"));

        let err = parse_mp4ff_key_specs(&["not-a-key".to_string()]).expect_err("len");
        assert_eq!(
            err.to_string(),
            "unpacking key: cannot decode key not-a-key"
        );
    }

    #[test]
    fn test_malformed_boxes_return_errors() {
        let dir = tempfile::tempdir().expect("tempdir");
        let out = dir.path().join("out.mp4");
        let cases: &[(&str, &[u8])] = &[
            ("empty", b""),
            ("short", &[0, 0, 0]),
            ("size0", b"\x00\x00\x00\x00free"),
            ("huge", b"\x7f\xff\xff\xffmdat"),
            ("free", b"\x00\x00\x00\x08free"),
        ];
        for (name, bytes) in cases {
            let input = dir.path().join(format!("{name}.mp4"));
            std::fs::write(&input, bytes).unwrap();
            let err = decrypt_with_mp4ff(&[key()], &input, &out).expect_err(name);
            assert!(
                !err.to_string().is_empty(),
                "{name} returned an empty error"
            );
            assert!(!out.exists(), "{name} left an output file");
        }

        let input = dir.path().join("moof.mp4");
        std::fs::write(&input, b"\x00\x00\x00\x08moof\x00\x00\x00\x08mdat").unwrap();
        let err = decrypt_with_mp4ff(&[key()], &input, &out).expect_err("moof");
        assert_eq!(
            err.to_string(),
            "mp4ff decryption failed: no init segment file and no init part of file"
        );

        let input = dir.path().join("free.mp4");
        std::fs::write(&input, b"\x00\x00\x00\x08free").unwrap();
        let err = decrypt_with_mp4ff(&[key()], &input, &out).expect_err("progressive");
        assert_eq!(
            err.to_string(),
            "mp4ff decryption failed: file not fragmented. Not supported"
        );
    }
}
