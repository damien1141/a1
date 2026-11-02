//! The `pxb_message!` declarative macro.
//!
//! Generates a `#[derive(Debug, Clone, PartialEq, Eq, Default)]` struct with
//! `pub` fields, plus `encode(&self) -> Vec<u8>` and
//! `decode(b: &[u8]) -> Result<Self, Error>` methods, from a compact field
//! list. The `option` keyword marks a field as wire-optional: it is omitted
//! from the encoding when [`WireField::is_opt_zero`] returns `true`.

macro_rules! pxb_message {
    ($(#[$meta:meta])* pub struct $Name:ident { $($body:tt)* }) => {
        pxb_message!(@step $Name, [$(#[$meta])*], [], [], [ $($body)* ]);
    };

    // Field descriptors are space-separated triples: "+" or "?" tag field
    (@step $Name:ident, [$(#[$meta:meta])*],
     [$($sf:tt)*], [$($op:tt $ftag:literal $ffield:ident)*], []) => {
        #[derive(Debug, Clone, PartialEq, Eq, Default)]
        $(#[$meta])*
        pub struct $Name { $($sf)* }
        impl $Name {
            pub fn encode(&self) -> Vec<u8> {
                let mut fw = FieldWriter::new();
                $( pxb_message!(@enc self, fw, $op, $ftag, $ffield); )*
                fw.into_vec()
            }
            pub fn decode(b: &[u8]) -> Result<Self, Error> {
                let mut m = Self::default();
                walk_fields(b, |tag, kind, fr| {
                    // macro_rules cannot expand to match arms, so repeat here.
                    match tag {
                        $( $ftag => { m.$ffield = WireField::decode_field(kind, fr)?; } )*
                        _ => fr.skip(kind)?,
                    }
                    Ok(())
                })?;
                Ok(m)
            }
        }
    };

    (@step $Name:ident, [$(#[$meta:meta])*],
     [$($sf:tt)*], [$($op:tt $ftag:literal $ffield:ident)*],
     [$(#[$attr:meta])* $tag:literal => $field:ident : $ty:ty , $($rest:tt)*]) => {
        pxb_message!(@step $Name, [$(#[$meta])*],
            [$($sf)* $(#[$attr])* pub $field: $ty,],
            [$($op $ftag $ffield)* + $tag $field],
            [$($rest)*]
        );
    };

    (@step $Name:ident, [$(#[$meta:meta])*],
     [$($sf:tt)*], [$($op:tt $ftag:literal $ffield:ident)*],
     [$(#[$attr:meta])* option $tag:literal => $field:ident : $ty:ty , $($rest:tt)*]) => {
        pxb_message!(@step $Name, [$(#[$meta])*],
            [$($sf)* $(#[$attr])* pub $field: $ty,],
            [$($op $ftag $ffield)* ? $tag $field],
            [$($rest)*]
        );
    };

    (@enc $self:ident, $fw:ident, +, $tag:literal, $field:ident) => {
        WireField::encode_field(&$self.$field, &mut $fw, $tag);
    };
    (@enc $self:ident, $fw:ident, ?, $tag:literal, $field:ident) => {
        if !$self.$field.is_opt_zero() {
            WireField::encode_field(&$self.$field, &mut $fw, $tag);
        }
    };
}
