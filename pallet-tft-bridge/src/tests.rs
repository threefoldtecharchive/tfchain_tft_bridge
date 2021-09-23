// use crate::{mock::*, Error, RawEvent};
use frame_support::{assert_noop, assert_ok};
use frame_system::{RawOrigin};
use crate::{mock::*, Error};
use sp_runtime::{DispatchError};

#[test]
fn add_validator_works() {
	new_test_ext().execute_with(|| {
        assert_ok!(TFTBridgeModule::add_validator(RawOrigin::Root.into(), bob()));
        assert_ok!(TFTBridgeModule::add_validator(RawOrigin::Root.into(), alice()));
	});
}

#[test]
fn add_validator_non_root_fails() {
	new_test_ext().execute_with(|| {
        assert_noop!(
            TFTBridgeModule::add_validator(Origin::signed(alice()), bob()),
            DispatchError::BadOrigin
        );
	});
}

#[test]
fn removing_validator_works() {
	new_test_ext().execute_with(|| {
        assert_ok!(TFTBridgeModule::add_validator(RawOrigin::Root.into(), bob()));
        assert_ok!(TFTBridgeModule::remove_validator(RawOrigin::Root.into(), bob()));
	});
}

#[test]
fn proposing_mint_transaction_works() {
	new_test_ext().execute_with(|| {
        assert_ok!(TFTBridgeModule::add_validator(RawOrigin::Root.into(), alice()));

        assert_ok!(TFTBridgeModule::propose_or_vote_mint_transaction(Origin::signed(alice()), "some_tx".as_bytes().to_vec(), bob(), 2));        
	});
}

#[test]
fn proposing_mint_transaction_without_being_validator_fails() {
	new_test_ext().execute_with(|| {
        assert_noop!(
            TFTBridgeModule::propose_or_vote_mint_transaction(Origin::signed(alice()), "some_tx".as_bytes().to_vec(), bob(), 2),
            Error::<TestRuntime>::ValidatorNotExists
        );
	});
}

#[test]
fn mint_flow() {
	new_test_ext().execute_with(|| {
        prepare_validators();

        assert_ok!(TFTBridgeModule::propose_or_vote_mint_transaction(Origin::signed(alice()), "some_tx".as_bytes().to_vec(), bob(), 2));     

        assert_ok!(TFTBridgeModule::propose_or_vote_mint_transaction(Origin::signed(bob()), "some_tx".as_bytes().to_vec(), bob(), 2));     
        
        let mint_tx = TFTBridgeModule::mint_transactions("some_tx".as_bytes().to_vec());
        assert_eq!(mint_tx.votes, 2);

        assert_ok!(TFTBridgeModule::propose_or_vote_mint_transaction(Origin::signed(eve()), "some_tx".as_bytes().to_vec(), bob(), 2));
        let executed_mint_tx = TFTBridgeModule::executed_mint_transactions("some_tx".as_bytes().to_vec());
        assert_eq!(executed_mint_tx.votes, 3);
	});
}

#[test]
fn proposing_burn_transaction_works() {
	new_test_ext().execute_with(|| {
        prepare_validators();

        assert_ok!(TFTBridgeModule::propose_burn_transaction_or_add_sig(Origin::signed(alice()), 1, bob(), 2, "some_sig".as_bytes().to_vec(), "some_stellar_pubkey".as_bytes().to_vec()));        
	});
}

#[test]
fn proposing_burn_transaction_without_being_validator_fails() {
	new_test_ext().execute_with(|| {
        assert_noop!(
            TFTBridgeModule::propose_burn_transaction_or_add_sig(Origin::signed(alice()), 1, bob(), 2, "some_sig".as_bytes().to_vec(), "some_stellar_pubkey".as_bytes().to_vec()),
            Error::<TestRuntime>::ValidatorNotExists
        );
	});
}

#[test]
fn burn_flow() {
	new_test_ext().execute_with(|| {
        prepare_validators();

        assert_ok!(TFTBridgeModule::propose_burn_transaction_or_add_sig(Origin::signed(alice()), 1, bob(), 2, "alice_sig".as_bytes().to_vec(), "alice_stellar_pubkey".as_bytes().to_vec()));     

        assert_ok!(TFTBridgeModule::propose_burn_transaction_or_add_sig(Origin::signed(bob()), 1, bob(), 2, "bob_sig".as_bytes().to_vec(), "bob_stellar_pubkey".as_bytes().to_vec()));     
        
        let burn_tx = TFTBridgeModule::burn_transactions(1);
        assert_eq!(burn_tx.signatures.len(), 2);

        assert_ok!(TFTBridgeModule::propose_burn_transaction_or_add_sig(Origin::signed(eve()), 1, bob(), 2, "some_other_eve_sig".as_bytes().to_vec(), "eve_stellar_pubkey".as_bytes().to_vec()));
        assert_ok!(TFTBridgeModule::propose_burn_transaction_or_add_sig(Origin::signed(ferdie()), 1, bob(), 2, "some_other_ferdie_sig".as_bytes().to_vec(), "ferdie_stellar_pubkey".as_bytes().to_vec()));
        let executed_burn_tx = TFTBridgeModule::burn_transactions(1);
        assert_eq!(executed_burn_tx.signatures.len(), 4);
	});
}

fn prepare_validators() {
	TFTBridgeModule::add_validator(RawOrigin::Root.into(), alice()).unwrap();
    TFTBridgeModule::add_validator(RawOrigin::Root.into(), bob()).unwrap();
	TFTBridgeModule::add_validator(RawOrigin::Root.into(), eve()).unwrap();
	TFTBridgeModule::add_validator(RawOrigin::Root.into(), ferdie()).unwrap();
}