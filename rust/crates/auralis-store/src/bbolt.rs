//! Pure-Rust read-only parser for legacy bbolt / BoltDB database files.

use auralis_core::error::store::StoreError;
use std::collections::HashMap;

const BOLT_MAGIC: u32 = 0xED0CDAED;
const PAGE_HEADER_SIZE: usize = 16;
const LEAF_ELEMENT_SIZE: usize = 16;
const BRANCH_ELEMENT_SIZE: usize = 16;

const BRANCH_PAGE_FLAG: u16 = 0x04;
const LEAF_PAGE_FLAG: u16 = 0x02;
const META_PAGE_FLAG: u16 = 0x10;

#[derive(Debug, Clone)]
pub struct Meta {
    pub magic: u32,
    pub version: u32,
    pub page_size: u32,
    pub root_pgid: u64,
    pub txid: u64,
}

pub struct BoltReader<'a> {
    data: &'a [u8],
    page_size: usize,
    meta: Meta,
}

impl<'a> BoltReader<'a> {
    pub fn new(data: &'a [u8]) -> Result<Self, StoreError> {
        if data.len() < 4096 * 2 {
            return Err(StoreError::LegacyParse("File too small for BoltDB".into()));
        }

        let meta0 = Self::read_meta_page(data, 0, 4096);
        let meta1 = Self::read_meta_page(data, 1, 4096);

        let meta = match (meta0, meta1) {
            (Ok(m0), Ok(m1)) => {
                if m1.txid > m0.txid {
                    m1
                } else {
                    m0
                }
            }
            (Ok(m0), Err(_)) => m0,
            (Err(_), Ok(m1)) => m1,
            (Err(e), Err(_)) => return Err(e),
        };

        let page_size = meta.page_size as usize;
        if !(512..=65536).contains(&page_size) || !page_size.is_power_of_two() {
            return Err(StoreError::LegacyParse(format!(
                "Invalid page size: {}",
                page_size
            )));
        }

        Ok(Self {
            data,
            page_size,
            meta,
        })
    }

    fn read_meta_page(data: &[u8], page_id: usize, page_size: usize) -> Result<Meta, StoreError> {
        let offset = page_id * page_size;
        if data.len() < offset + page_size {
            return Err(StoreError::LegacyParse("Buffer too short for meta page".into()));
        }

        let flags = u16::from_le_bytes([data[offset + 8], data[offset + 9]]);
        if flags != META_PAGE_FLAG {
            return Err(StoreError::LegacyParse(format!(
                "Page {} is not a meta page (flags: {:#x})",
                page_id, flags
            )));
        }

        let meta_offset = offset + PAGE_HEADER_SIZE;
        let magic = u32::from_le_bytes(data[meta_offset..meta_offset + 4].try_into().unwrap());
        if magic != BOLT_MAGIC {
            return Err(StoreError::LegacyParse(format!(
                "Invalid magic number: {:#x}",
                magic
            )));
        }

        let version =
            u32::from_le_bytes(data[meta_offset + 4..meta_offset + 8].try_into().unwrap());
        let page_size =
            u32::from_le_bytes(data[meta_offset + 8..meta_offset + 12].try_into().unwrap());
        // root bucket: root: u64 (8 bytes), sequence: u64 (8 bytes)
        let root_pgid =
            u64::from_le_bytes(data[meta_offset + 16..meta_offset + 24].try_into().unwrap());
        let txid =
            u64::from_le_bytes(data[meta_offset + 40..meta_offset + 48].try_into().unwrap());

        Ok(Meta {
            magic,
            version,
            page_size,
            root_pgid,
            txid,
        })
    }

    /// Reads all key-value pairs in a top-level bucket by name.
    pub fn read_bucket(&self, bucket_name: &[u8]) -> Result<HashMap<Vec<u8>, Vec<u8>>, StoreError> {
        let mut top_buckets = HashMap::new();
        self.traverse_bucket(self.meta.root_pgid, &mut top_buckets)?;

        let Some(bucket_bytes) = top_buckets.get(bucket_name) else {
            return Ok(HashMap::new()); // Bucket not found, return empty map
        };

        if bucket_bytes.len() < 16 {
            return Err(StoreError::LegacyParse(
                "Invalid bucket header bytes".into(),
            ));
        }

        let bucket_root_pgid = u64::from_le_bytes(bucket_bytes[0..8].try_into().unwrap());
        let mut entries = HashMap::new();
        self.traverse_bucket(bucket_root_pgid, &mut entries)?;
        Ok(entries)
    }

    /// Reads all top-level bucket names in the database.
    pub fn list_buckets(&self) -> Result<Vec<Vec<u8>>, StoreError> {
        let mut top_buckets = HashMap::new();
        self.traverse_bucket(self.meta.root_pgid, &mut top_buckets)?;
        Ok(top_buckets.into_keys().collect())
    }

    fn traverse_bucket(
        &self,
        pgid: u64,
        out: &mut HashMap<Vec<u8>, Vec<u8>>,
    ) -> Result<(), StoreError> {
        let offset = pgid as usize * self.page_size;
        if self.data.len() < offset + PAGE_HEADER_SIZE {
            return Err(StoreError::LegacyParse("Offset out of bounds".into()));
        }

        let flags = u16::from_le_bytes([self.data[offset + 8], self.data[offset + 9]]);
        let count = u16::from_le_bytes([self.data[offset + 10], self.data[offset + 11]]) as usize;

        if flags & LEAF_PAGE_FLAG != 0 {
            for i in 0..count {
                let elem_offset = offset + PAGE_HEADER_SIZE + (i * LEAF_ELEMENT_SIZE);
                if self.data.len() < elem_offset + LEAF_ELEMENT_SIZE {
                    return Err(StoreError::LegacyParse("Leaf element out of bounds".into()));
                }

                let pos = u32::from_le_bytes(
                    self.data[elem_offset + 4..elem_offset + 8]
                        .try_into()
                        .unwrap(),
                ) as usize;
                let ksize = u32::from_le_bytes(
                    self.data[elem_offset + 8..elem_offset + 12]
                        .try_into()
                        .unwrap(),
                ) as usize;
                let vsize = u32::from_le_bytes(
                    self.data[elem_offset + 12..elem_offset + 16]
                        .try_into()
                        .unwrap(),
                ) as usize;

                let key_start = elem_offset + pos;
                let key_end = key_start + ksize;
                let val_start = key_end;
                let val_end = val_start + vsize;

                if self.data.len() < val_end {
                    return Err(StoreError::LegacyParse("Key/val slice out of bounds".into()));
                }

                let key = self.data[key_start..key_end].to_vec();
                let val = self.data[val_start..val_end].to_vec();
                out.insert(key, val);
            }
        } else if flags & BRANCH_PAGE_FLAG != 0 {
            for i in 0..count {
                let elem_offset = offset + PAGE_HEADER_SIZE + (i * BRANCH_ELEMENT_SIZE);
                if self.data.len() < elem_offset + BRANCH_ELEMENT_SIZE {
                    return Err(StoreError::LegacyParse(
                        "Branch element out of bounds".into(),
                    ));
                }

                let child_pgid = u64::from_le_bytes(
                    self.data[elem_offset..elem_offset + 8].try_into().unwrap(),
                );
                self.traverse_bucket(child_pgid, out)?;
            }
        }

        Ok(())
    }
}

#[cfg(test)]
type TestBucketDef<'a> = (&'a [u8], &'a [(&'a [u8], &'a [u8])]);

/// Helper to build a valid, deterministic bbolt database byte buffer for tests and fixtures.
#[cfg(test)]
pub fn build_test_bbolt_db(buckets: &[TestBucketDef]) -> Vec<u8> {
    const PAGE_SZ: usize = 4096;
    let total_pages = 3 + buckets.len();
    let mut data = vec![0u8; total_pages * PAGE_SZ];

    // Page 0: Meta 0 (txid = 1)
    write_meta_page(&mut data[0..PAGE_SZ], 0, 1);
    // Page 1: Meta 1 (txid = 2)
    write_meta_page(&mut data[PAGE_SZ..2 * PAGE_SZ], 1, 2);

    // Page 2: Root Bucket leaf page
    let root_page_offset = 2 * PAGE_SZ;
    write_leaf_page_header(
        &mut data[root_page_offset..root_page_offset + PAGE_HEADER_SIZE],
        2,
        buckets.len() as u16,
    );

    let root_data_start = root_page_offset + PAGE_HEADER_SIZE + buckets.len() * LEAF_ELEMENT_SIZE;
    let mut current_data_offset = root_data_start;

    for (i, (bucket_name, _)) in buckets.iter().enumerate() {
        let child_pgid = (3 + i) as u64;
        let elem_offset = root_page_offset + PAGE_HEADER_SIZE + i * LEAF_ELEMENT_SIZE;
        let pos = (current_data_offset - elem_offset) as u32;
        let ksize = bucket_name.len() as u32;
        let vsize = 16u32;

        // Leaf element: flags = 1 (bucket), pos, ksize, vsize
        data[elem_offset..elem_offset + 4].copy_from_slice(&1u32.to_le_bytes());
        data[elem_offset + 4..elem_offset + 8].copy_from_slice(&pos.to_le_bytes());
        data[elem_offset + 8..elem_offset + 12].copy_from_slice(&ksize.to_le_bytes());
        data[elem_offset + 12..elem_offset + 16].copy_from_slice(&vsize.to_le_bytes());

        // Key
        data[current_data_offset..current_data_offset + bucket_name.len()]
            .copy_from_slice(bucket_name);
        current_data_offset += bucket_name.len();

        // Bucket header (16 bytes: root_pgid + sequence)
        data[current_data_offset..current_data_offset + 8]
            .copy_from_slice(&child_pgid.to_le_bytes());
        data[current_data_offset + 8..current_data_offset + 16]
            .copy_from_slice(&0u64.to_le_bytes());
        current_data_offset += 16;
    }

    // Child bucket pages: pgid = 3 + i
    for (i, (_, entries)) in buckets.iter().enumerate() {
        let child_pgid = (3 + i) as u64;
        let page_offset = (child_pgid as usize) * PAGE_SZ;

        write_leaf_page_header(
            &mut data[page_offset..page_offset + PAGE_HEADER_SIZE],
            child_pgid,
            entries.len() as u16,
        );

        let data_start = page_offset + PAGE_HEADER_SIZE + entries.len() * LEAF_ELEMENT_SIZE;
        let mut child_data_offset = data_start;

        for (j, (k, v)) in entries.iter().enumerate() {
            let elem_offset = page_offset + PAGE_HEADER_SIZE + j * LEAF_ELEMENT_SIZE;
            let pos = (child_data_offset - elem_offset) as u32;
            let ksize = k.len() as u32;
            let vsize = v.len() as u32;

            data[elem_offset..elem_offset + 4].copy_from_slice(&0u32.to_le_bytes());
            data[elem_offset + 4..elem_offset + 8].copy_from_slice(&pos.to_le_bytes());
            data[elem_offset + 8..elem_offset + 12].copy_from_slice(&ksize.to_le_bytes());
            data[elem_offset + 12..elem_offset + 16].copy_from_slice(&vsize.to_le_bytes());

            data[child_data_offset..child_data_offset + k.len()].copy_from_slice(k);
            child_data_offset += k.len();

            data[child_data_offset..child_data_offset + v.len()].copy_from_slice(v);
            child_data_offset += v.len();
        }
    }

    data
}

#[cfg(test)]
fn write_meta_page(page: &mut [u8], id: u64, txid: u64) {
    // Header
    page[0..8].copy_from_slice(&id.to_le_bytes());
    page[8..10].copy_from_slice(&META_PAGE_FLAG.to_le_bytes());
    page[10..12].copy_from_slice(&0u16.to_le_bytes());
    page[12..16].copy_from_slice(&0u32.to_le_bytes());

    // Meta body at offset 16
    let m = &mut page[PAGE_HEADER_SIZE..];
    m[0..4].copy_from_slice(&BOLT_MAGIC.to_le_bytes());
    m[4..8].copy_from_slice(&2u32.to_le_bytes()); // version
    m[8..12].copy_from_slice(&4096u32.to_le_bytes()); // page_size
    m[12..16].copy_from_slice(&0u32.to_le_bytes()); // flags
    m[16..24].copy_from_slice(&2u64.to_le_bytes()); // root bucket pgid = 2
    m[24..32].copy_from_slice(&0u64.to_le_bytes()); // root bucket sequence = 0
    m[32..40].copy_from_slice(&0u64.to_le_bytes()); // freelist
    m[40..48].copy_from_slice(&txid.to_le_bytes()); // txid
}

#[cfg(test)]
fn write_leaf_page_header(header: &mut [u8], id: u64, count: u16) {
    header[0..8].copy_from_slice(&id.to_le_bytes());
    header[8..10].copy_from_slice(&LEAF_PAGE_FLAG.to_le_bytes());
    header[10..12].copy_from_slice(&count.to_le_bytes());
    header[12..16].copy_from_slice(&0u32.to_le_bytes());
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_bbolt_fixture_round_trip() {
        let fixture = build_test_bbolt_db(&[
            (b"queue", &[(b"k1", b"v1"), (b"k2", b"v2")]),
            (b"isrc", &[(b"USRC1700001", b"spotify_123")]),
        ]);

        let reader = BoltReader::new(&fixture).expect("valid bbolt reader");
        let buckets = reader.list_buckets().expect("list buckets");
        assert_eq!(buckets.len(), 2);

        let queue_entries = reader.read_bucket(b"queue").expect("read queue");
        assert_eq!(queue_entries.len(), 2);
        assert_eq!(queue_entries.get(b"k1".as_slice()).unwrap(), b"v1");
        assert_eq!(queue_entries.get(b"k2".as_slice()).unwrap(), b"v2");

        let isrc_entries = reader.read_bucket(b"isrc").expect("read isrc");
        assert_eq!(isrc_entries.len(), 1);
        assert_eq!(
            isrc_entries.get(b"USRC1700001".as_slice()).unwrap(),
            b"spotify_123"
        );
    }
}
