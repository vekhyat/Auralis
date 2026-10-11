//! Auralis music provider clients, metadata resolution, crypto, and auth.

pub mod amazon;
pub mod antra;
pub mod apple;
pub mod bounded_body;
pub mod deezer;
pub mod deezer_crypto;
pub mod dispatcher;
pub mod error;
pub mod http_headers;
pub mod jiosaavn;
pub mod link_resolver;
pub mod models;
pub mod mp4ff;
pub mod provider_cooldown;
pub mod provider_endpoints;
pub mod qobuz;
pub mod songlink;
pub mod songstats;
pub mod spotify;
pub mod spotify_totp;
pub mod tidal;
pub mod zarz;
