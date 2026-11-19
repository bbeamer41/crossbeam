# Copyright (C) 2026 K5LEB
# SPDX-License-Identifier: GPL-3.0-or-later
"""The LXMF bridge answers only a message whose signature validates."""

import importlib.machinery
import importlib.util
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent


def load_bridge():
    path = HERE / "crossbeam-lxmf-ai"
    loader = importlib.machinery.SourceFileLoader("crossbeam_lxmf_ai", str(path))
    spec = importlib.util.spec_from_loader("crossbeam_lxmf_ai", loader)
    mod = importlib.util.module_from_spec(spec)
    loader.exec_module(mod)
    return mod


class SignatureTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.bridge = load_bridge()

    def test_accepts_only_a_validated_signature(self):
        ok = self.bridge.signature_accepted
        invalid = self.bridge.SIGNATURE_INVALID
        self.assertTrue(ok(True, None, invalid))
        self.assertFalse(ok(False, None, invalid))
        self.assertFalse(ok(False, 0x01, invalid))
        self.assertFalse(ok(False, invalid, invalid))
        self.assertFalse(ok(True, invalid, invalid))
        self.assertFalse(ok(1, None, invalid))

    def test_live_lxmf_reasons(self):
        try:
            import LXMF
            import RNS
        except ImportError:
            self.skipTest("rns and lxmf are not installed")
        self.assertEqual(LXMF.LXMessage.SIGNATURE_INVALID, 0x02)
        self.assertEqual(LXMF.LXMessage.SOURCE_UNKNOWN, 0x01)
        ident_a = RNS.Identity()
        ident_b = RNS.Identity()
        dest_hash = RNS.Destination.hash(ident_b, "lxmf", "delivery")
        src_hash = RNS.Destination.hash(ident_a, "lxmf", "delivery")
        RNS.Identity.remember(None, dest_hash, ident_b.get_public_key())
        RNS.Identity.remember(None, src_hash, ident_a.get_public_key())
        src = RNS.Destination(ident_a, RNS.Destination.OUT, RNS.Destination.SINGLE, "lxmf", "delivery")
        dst = RNS.Destination(ident_b, RNS.Destination.OUT, RNS.Destination.SINGLE, "lxmf", "delivery")
        msg = LXMF.LXMessage(dst, src, "AI hello", title="t")
        msg.pack()
        good = LXMF.LXMessage.unpack_from_bytes(msg.packed)
        self.assertTrue(self.bridge.signature_accepted(good.signature_validated, good.unverified_reason))

        bad = bytearray(msg.packed)
        bad[40] ^= 0xFF
        invalid = LXMF.LXMessage.unpack_from_bytes(bytes(bad))
        self.assertFalse(self.bridge.signature_accepted(invalid.signature_validated, invalid.unverified_reason))
        self.assertEqual(invalid.unverified_reason, LXMF.LXMessage.SIGNATURE_INVALID)

        unknown = RNS.Identity()
        usrc = RNS.Destination(unknown, RNS.Destination.OUT, RNS.Destination.SINGLE, "lxmf", "delivery")
        msg2 = LXMF.LXMessage(dst, usrc, "AI unsigned", title="t")
        msg2.pack()
        unsigned = LXMF.LXMessage.unpack_from_bytes(msg2.packed)
        self.assertFalse(unsigned.signature_validated)
        self.assertFalse(self.bridge.signature_accepted(unsigned.signature_validated, unsigned.unverified_reason))
        self.assertNotEqual(unsigned.unverified_reason, LXMF.LXMessage.SIGNATURE_INVALID)


if __name__ == "__main__":
    unittest.main()
