"""Check that keyboard controls reject unrelated faults and preserve cancellation."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from buildflow.contracts import BuildError
import native_verify


class NegativeControlTests(unittest.TestCase):
    def check_control(self, failure=None, code=1):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            def failed(args, app, directory, harness, negative=False):
                if failure is not None:
                    path = directory / 'browser/failure.json'
                    path.parent.mkdir(parents=True)
                    path.write_text(json.dumps(failure))
                raise BuildError('original command failure', code)
            with patch.object(native_verify, 'browser_check', side_effect=failed):
                native_verify.negative_check(None, None, output, None)
            return json.loads((output / 'negative-image-key/control.json').read_text())

    def evidence(self, report_events=False):
        press = '\x1b[118;5u'
        repeat = '\x1b[118;5:2u' if report_events else None
        release = '\x1b[118;5:3u' if report_events else None
        return {'code': 'keyboard.fallback.swallowed', 'error': 'Ctrl+V fallback key frames were swallowed',
                'failures': [], 'mutation': {'id': 'disable-native-paste-key', 'applied': True},
                'evidence': {'expected': [press, repeat, release] if report_events else [press, '\x16'],
                             'actual': [], 'events': [
                    {'type': 'keydown', 'repeat': False, 'encoded': press},
                    {'type': 'keydown', 'repeat': True, 'encoded': repeat},
                    {'type': 'keyup', 'repeat': False, 'encoded': release}]}}

    def test_specific_injected_key_failure_is_accepted(self):
        for report_events in (False, True):
            with self.subTest(report_events=report_events):
                failure = self.evidence(report_events)
                control = self.check_control(failure)
                self.assertTrue(control['verified'])
                self.assertEqual(control['expected_frames'], failure['evidence']['expected'])
                self.assertEqual(control['actual_frames'], [])

    def test_legacy_key_fallback_is_accepted(self):
        failure = self.evidence()
        for event in failure['evidence']['events']:
            event['encoded'] = None
        failure['evidence']['expected'] = ['\x16', '\x16']
        self.assertTrue(self.check_control(failure)['verified'])

    def test_frames_must_match_event_encoding_and_order(self):
        for expected in ([], ['press', 'repeat', 'release'], ['\x16', '\x1b[118;5u'],
                         ['\x1b[118;5u'], ['\x1b[118;5u', '\x16', '\x16']):
            with self.subTest(expected=expected):
                failure = self.evidence()
                failure['evidence']['expected'] = expected
                with self.assertRaisesRegex(ValueError, 'expected_frames'):
                    self.check_control(failure)

    def test_partial_swallowing_is_rejected(self):
        failure = self.evidence()
        failure['evidence']['actual'] = [failure['evidence']['expected'][0]]
        with self.assertRaisesRegex(ValueError, 'missing_frames'):
            self.check_control(failure)

    def test_event_sequence_must_include_press_repeat_release(self):
        for events in ([], [{'type': 'keydown', 'repeat': False}],
                       list(reversed(self.evidence()['evidence']['events']))):
            with self.subTest(events=events):
                failure = self.evidence()
                failure['evidence']['events'] = events
                with self.assertRaisesRegex(ValueError, 'event_sequence'):
                    self.check_control(failure)

    def test_same_message_without_injection_proof_is_rejected(self):
        failure = self.evidence()
        failure['mutation']['applied'] = False
        with self.assertRaisesRegex(ValueError, 'observed'):
            self.check_control(failure)

    def test_page_errors_and_other_assertions_remain_failures(self):
        for patch_value in ({'code': None, 'error': 'settings did not close'}, {'failures': ['page error']}):
            with self.assertRaisesRegex(ValueError, 'observed'):
                self.check_control({**self.evidence(), **patch_value})

    def test_missing_evidence_does_not_reuse_previous_failure(self):
        with self.assertRaisesRegex(ValueError, 'no failure evidence'):
            self.check_control()

    def test_cancellation_is_not_a_passing_negative_control(self):
        with self.assertRaises(BuildError) as raised:
            self.check_control(self.evidence(), code=130)
        self.assertEqual(raised.exception.exit_code, 130)


if __name__ == '__main__':
    unittest.main(verbosity=2)
