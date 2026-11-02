//! Zero-cost [`WireField`] trait for per-type PXB encode/decode.
//!
//! Each primitive (`String`, `bool`, `u16`, `u32`, `Vec<u8>`, `Vec<u16>`)
//! implements the trait once; the [`pxb_message!`] macro monomorphizes through
//! it at compile time — no dynamic dispatch, no per-message hand-written
//! encode/decode boilerplate.

use super::codec::Error;
use super::fields::{FieldReader, FieldWriter, WIRE_BYTES, WIRE_U64};

pub(super) trait WireField {
    fn encode_field(&self, fw: &mut FieldWriter, tag: u16);
    fn decode_field(kind: u8, fr: &mut FieldReader<'_>) -> Result<Self, Error>
    where
        Self: Sized;
    fn is_opt_zero(&self) -> bool {
        false
    }
}

fn take_u64(kind: u8, fr: &mut FieldReader<'_>) -> Result<u64, Error> {
    if kind != WIRE_U64 {
        fr.skip(kind)?;
        return Err(Error::BadWire);
    }
    fr.u64()
}

fn take_bytes<'a>(kind: u8, fr: &mut FieldReader<'a>) -> Result<&'a [u8], Error> {
    if kind != WIRE_BYTES {
        fr.skip(kind)?;
        return Err(Error::BadWire);
    }
    fr.bytes()
}

/// Wire strings are byte blobs; text fields surface as UTF-8 with invalid
/// sequences replaced (Go keeps raw bytes, which is unusable for `String`).
fn take_string(kind: u8, fr: &mut FieldReader<'_>) -> Result<String, Error> {
    Ok(String::from_utf8_lossy(take_bytes(kind, fr)?).into_owned())
}

fn decode_u16s(p: &[u8]) -> Result<Vec<u16>, Error> {
    // Inner format is plain ByteReader: u16 count + u16 values (no tags).
    if p.len() < 2 {
        return Err(Error::Truncated);
    }
    let n = u16::from_le_bytes([p[0], p[1]]) as usize;
    let data = p.get(2..2 + n * 2).ok_or(Error::Truncated)?;
    Ok(data
        .chunks_exact(2)
        .map(|c| u16::from_le_bytes([c[0], c[1]]))
        .collect())
}

impl WireField for String {
    fn encode_field(&self, fw: &mut FieldWriter, tag: u16) {
        fw.put_string(tag, self);
    }
    fn decode_field(kind: u8, fr: &mut FieldReader<'_>) -> Result<Self, Error> {
        take_string(kind, fr)
    }
}

impl WireField for bool {
    fn encode_field(&self, fw: &mut FieldWriter, tag: u16) {
        fw.put_bool(tag, *self);
    }
    fn decode_field(kind: u8, fr: &mut FieldReader<'_>) -> Result<Self, Error> {
        Ok(take_u64(kind, fr)? != 0)
    }
    fn is_opt_zero(&self) -> bool {
        !*self
    }
}

impl WireField for u16 {
    fn encode_field(&self, fw: &mut FieldWriter, tag: u16) {
        fw.put_u16(tag, *self);
    }
    fn decode_field(kind: u8, fr: &mut FieldReader<'_>) -> Result<Self, Error> {
        Ok(take_u64(kind, fr)? as u16)
    }
}

impl WireField for u32 {
    fn encode_field(&self, fw: &mut FieldWriter, tag: u16) {
        fw.put_u32(tag, *self);
    }
    fn decode_field(kind: u8, fr: &mut FieldReader<'_>) -> Result<Self, Error> {
        Ok(take_u64(kind, fr)? as u32)
    }
    fn is_opt_zero(&self) -> bool {
        *self == 0
    }
}

impl WireField for Vec<u8> {
    fn encode_field(&self, fw: &mut FieldWriter, tag: u16) {
        fw.put_bytes(tag, self);
    }
    fn decode_field(kind: u8, fr: &mut FieldReader<'_>) -> Result<Self, Error> {
        Ok(take_bytes(kind, fr)?.to_vec())
    }
}

impl WireField for Vec<u16> {
    fn encode_field(&self, fw: &mut FieldWriter, tag: u16) {
        fw.put_u16s(tag, self);
    }
    fn decode_field(kind: u8, fr: &mut FieldReader<'_>) -> Result<Self, Error> {
        decode_u16s(take_bytes(kind, fr)?)
    }
}
