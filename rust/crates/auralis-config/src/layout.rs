//! Layout and categorization for configuration settings.

use auralis_core::constants::CONFIG_LAYOUT_VERSION;
use serde_json::{json, Map, Value};
use std::collections::{HashMap, HashSet};

pub struct ConfigSectionDef {
    pub page: &'static str,
    pub section: &'static str,
    pub keys: &'static [&'static str],
}

pub const CONFIG_SECTIONS: &[ConfigSectionDef] = &[
    ConfigSectionDef {
        page: "settingsPage",
        section: "general",
        keys: &[
            "downloadPath",
            "language",
            "baseColor",
            "theme",
            "themeMode",
            "fontFamily",
            "operatingSystem",
            "sfxEnabled",
            "previewVolume",
            "showUpdateNotifications",
        ],
    },
    ConfigSectionDef {
        page: "settingsPage",
        section: "naming",
        keys: &[
            "folderPreset",
            "folderTemplate",
            "applyFolderToSingleTrack",
            "filenamePreset",
            "filenameTemplate",
            "albumFilenameTemplate",
            "useSeparateAlbumFilename",
            "trackNumber",
        ],
    },
    ConfigSectionDef {
        page: "settingsPage",
        section: "fileManagement",
        keys: &[
            "createPlaylistFolder",
            "playlistOwnerFolderName",
            "createM3u8File",
            "saveCover",
            "exportLogsFile",
            "exportLogsOnlyFailed",
            "autoConvertAudio",
            "autoConvertFormat",
            "autoConvertBitrate",
            "autoConvertDeleteOriginal",
            "autoResampleAudio",
            "autoResampleSampleRate",
            "autoResampleBitDepth",
            "autoResampleDeleteOriginal",
            "autoReplayGainTags",
            "autoReplayGainMode",
            "redownloadWithSuffix",
            "existingFileCheckMode",
            "metadataDateFormat",
            "metadataTags",
        ],
    },
    ConfigSectionDef {
        page: "settingsPage",
        section: "metadata",
        keys: &[
            "embedLyrics",
            "embedMaxQualityCover",
            "useFirstArtistOnly",
            "useSingleGenre",
            "embedGenre",
            "separator",
        ],
    },
    ConfigSectionDef {
        page: "workflowPage",
        section: "mode",
        keys: &["downloader", "autoQuality", "allowFallback"],
    },
    ConfigSectionDef {
        page: "workflowPage",
        section: "ordering",
        keys: &["autoOrder"],
    },
    ConfigSectionDef {
        page: "workflowPage",
        section: "linkResolvers",
        keys: &["linkResolver", "allowResolverFallback"],
    },
    ConfigSectionDef {
        page: "sourcePage",
        section: "sources",
        keys: &["customTidalApi", "customQobuzApi"],
    },
    ConfigSectionDef {
        page: "sourcePage",
        section: "quality",
        keys: &[
            "tidalQuality",
            "qobuzQuality",
            "amazonQuality",
            "allowAtmosFallback",
            "atmosFallbackQuality",
        ],
    },
];

pub fn get_known_config_keys() -> HashSet<&'static str> {
    let mut set = HashSet::new();
    for section in CONFIG_SECTIONS {
        for key in section.keys {
            set.insert(*key);
        }
    }
    set
}

pub fn default_settings() -> Map<String, Value> {
    let mut map = Map::new();
    map.insert("downloadPath".into(), json!(""));
    map.insert("language".into(), json!("en"));
    map.insert("downloader".into(), json!("auto"));
    map.insert("customTidalApi".into(), json!(""));
    map.insert("customQobuzApi".into(), json!(""));
    map.insert("linkResolver".into(), json!("songlink"));
    map.insert("allowResolverFallback".into(), json!(true));
    map.insert("baseColor".into(), json!("neutral"));
    map.insert("theme".into(), json!("neutral"));
    map.insert("themeMode".into(), json!("light"));
    map.insert("fontFamily".into(), json!("geist-sans"));
    map.insert("customFonts".into(), json!([]));
    map.insert("folderPreset".into(), json!("none"));
    map.insert("folderTemplate".into(), json!("{album_artist}/{album}"));
    map.insert("applyFolderToSingleTrack".into(), json!(false));
    map.insert("filenamePreset".into(), json!("title-artist"));
    map.insert("filenameTemplate".into(), json!("{title} - {artist}"));
    map.insert("albumFilenameTemplate".into(), json!("{track}. {title}"));
    map.insert("useSeparateAlbumFilename".into(), json!(false));
    map.insert("trackNumber".into(), json!(false));
    map.insert("sfxEnabled".into(), json!(true));
    map.insert("embedLyrics".into(), json!(false));
    map.insert("lyricsTranslationMode".into(), json!("off"));
    map.insert("lyricsTranslationLang".into(), json!("en"));
    map.insert("lyricsTranslationAutoFallback".into(), json!(true));
    map.insert("lrclibTitleFallback".into(), json!(true));
    map.insert("embedMaxQualityCover".into(), json!(false));

    #[cfg(target_os = "windows")]
    let os_name = "Windows";
    #[cfg(not(target_os = "windows"))]
    let os_name = "linux/MacOS";
    map.insert("operatingSystem".into(), json!(os_name));

    map.insert("tidalQuality".into(), json!("LOSSLESS"));
    map.insert("qobuzQuality".into(), json!("6"));
    map.insert("amazonQuality".into(), json!("16"));
    map.insert("autoOrder".into(), json!("tidal-qobuz-amazon"));
    map.insert("autoQuality".into(), json!("16"));
    map.insert("allowFallback".into(), json!(true));
    map.insert("allowAtmosFallback".into(), json!(true));
    map.insert("atmosFallbackQuality".into(), json!("24"));
    map.insert("createPlaylistFolder".into(), json!(true));
    map.insert("playlistOwnerFolderName".into(), json!(false));
    map.insert("createM3u8File".into(), json!(false));
    map.insert("saveCover".into(), json!(false));
    map.insert("exportLogsFile".into(), json!(true));
    map.insert("exportLogsOnlyFailed".into(), json!(false));
    map.insert("showUpdateNotifications".into(), json!(true));
    map.insert("previewVolume".into(), json!(100));
    map.insert("existingFileCheckMode".into(), json!("filename"));
    map.insert("autoConvertAudio".into(), json!(false));
    map.insert("autoConvertFormat".into(), json!("mp3"));
    map.insert("autoConvertBitrate".into(), json!("320k"));
    map.insert("autoConvertDeleteOriginal".into(), json!(false));
    map.insert("autoResampleAudio".into(), json!(false));
    map.insert("autoResampleSampleRate".into(), json!("44100"));
    map.insert("autoResampleBitDepth".into(), json!("16"));
    map.insert("autoResampleDeleteOriginal".into(), json!(false));
    map.insert("autoReplayGainTags".into(), json!(false));
    map.insert("autoReplayGainMode".into(), json!("album"));
    map.insert("useFirstArtistOnly".into(), json!(false));
    map.insert("useSingleGenre".into(), json!(false));
    map.insert("embedGenre".into(), json!(false));
    map.insert("redownloadWithSuffix".into(), json!(false));
    map.insert("separator".into(), json!("semicolon"));
    map.insert("metadataDateFormat".into(), json!("full"));
    map.insert(
        "metadataTags".into(),
        json!({
            "title": true,
            "artist": true,
            "album": true,
            "albumArtist": true,
            "date": true,
            "trackNumber": true,
            "discNumber": true,
            "genre": true,
            "composer": true,
            "copyright": true,
            "label": true,
            "isrc": true,
            "upc": true,
            "comment": true
        }),
    );

    map
}

/// Recursively flattens nested layout structure into a flat map of key -> value.
pub fn flatten_config_settings(config: &Map<String, Value>) -> Map<String, Value> {
    let mut flat = Map::new();
    let known = get_known_config_keys();

    fn visit(
        values: &Map<String, Value>,
        flat: &mut Map<String, Value>,
        known: &HashSet<&'static str>,
    ) {
        let mut keys: Vec<&String> = values.keys().collect();
        keys.sort();

        for key in keys {
            if key == "configVersion" {
                continue;
            }
            let value = &values[key];
            if known.contains(key.as_str()) {
                flat.insert(key.clone(), value.clone());
                continue;
            }
            if let Some(nested) = value.as_object() {
                visit(nested, flat, known);
                continue;
            }
            flat.insert(key.clone(), value.clone());
        }
    }

    visit(config, &mut flat, &known);

    for def in CONFIG_SECTIONS {
        if let Some(page) = config.get(def.page).and_then(|p| p.as_object()) {
            if let Some(sec) = page.get(def.section).and_then(|s| s.as_object()) {
                for key in def.keys {
                    if let Some(val) = sec.get(*key) {
                        flat.insert((*key).to_string(), val.clone());
                    }
                }
            }
        }
    }

    flat
}

/// Categorizes a flat or nested map into layout version 7 hierarchy.
pub fn categorize_config_settings(config: &Map<String, Value>) -> Map<String, Value> {
    let flat = flatten_config_settings(config);
    let mut grouped = Map::new();
    grouped.insert("configVersion".into(), json!(CONFIG_LAYOUT_VERSION));

    let mut used = HashSet::new();

    for def in CONFIG_SECTIONS {
        if !grouped.contains_key(def.page) {
            grouped.insert(def.page.into(), Value::Object(Map::new()));
        }
        let page_map = grouped.get_mut(def.page).unwrap().as_object_mut().unwrap();

        let mut section_map = Map::new();
        for key in def.keys {
            if let Some(val) = flat.get(*key) {
                section_map.insert((*key).to_string(), val.clone());
                used.insert(*key);
            }
        }
        page_map.insert(def.section.into(), Value::Object(section_map));
    }

    let mut other = Map::new();
    for (key, val) in &flat {
        if !used.contains(key.as_str()) {
            other.insert(key.clone(), val.clone());
        }
    }
    if !other.is_empty() {
        grouped.insert("application".into(), Value::Object(other));
    }

    grouped
}

fn config_path_orders() -> HashMap<String, Vec<&'static str>> {
    let mut orders = HashMap::new();
    orders.insert(
        "".to_string(),
        vec![
            "configVersion",
            "settingsPage",
            "workflowPage",
            "sourcePage",
            "application",
        ],
    );

    for def in CONFIG_SECTIONS {
        orders
            .entry(def.page.to_string())
            .or_default()
            .push(def.section);
        orders.insert(format!("{}.{}", def.page, def.section), def.keys.to_vec());
    }

    orders
}

fn make_ordered_value(
    value: &Value,
    path: &str,
    orders: &HashMap<String, Vec<&'static str>>,
) -> Value {
    let Some(obj) = value.as_object() else {
        return value.clone();
    };

    let mut keys: Vec<String> = obj.keys().cloned().collect();

    let ranks: HashMap<&'static str, usize> = orders
        .get(path)
        .map(|list| list.iter().enumerate().map(|(i, &k)| (k, i)).collect())
        .unwrap_or_default();

    keys.sort_by(|a, b| {
        let left_rank = ranks.get(a.as_str());
        let right_rank = ranks.get(b.as_str());
        match (left_rank, right_rank) {
            (Some(l), Some(r)) => l.cmp(r),
            (Some(_), None) => std::cmp::Ordering::Less,
            (None, Some(_)) => std::cmp::Ordering::Greater,
            (None, None) => a.cmp(b),
        }
    });

    let mut map = Map::new();
    for key in keys {
        let child_path = if path.is_empty() {
            key.clone()
        } else {
            format!("{}.{}", path, key)
        };
        let child_val = &obj[&key];
        map.insert(key, make_ordered_value(child_val, &child_path, orders));
    }

    Value::Object(map)
}

/// Serializes settings into canonical indented JSON adhering to Layout Version 7.
pub fn marshal_config_settings(config: &Map<String, Value>) -> Result<Vec<u8>, serde_json::Error> {
    let categorized = categorize_config_settings(config);
    let orders = config_path_orders();
    let ordered = make_ordered_value(&Value::Object(categorized), "", &orders);
    serde_json::to_vec_pretty(&ordered)
}
