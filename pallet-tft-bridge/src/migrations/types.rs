pub mod v1 {
    use crate::types::StellarSignature;
    use codec::{Decode, Encode};
    use scale_info::TypeInfo;
    use sp_std::vec::Vec;

    #[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Encode, Decode, Default, Debug, TypeInfo)]
    pub struct BurnTransaction<BlockNumber> {
        pub block: BlockNumber,
        pub amount: u64,
        pub target: Vec<u8>,
        pub signatures: Vec<StellarSignature>,
        pub sequence_number: u64,
    }
}
