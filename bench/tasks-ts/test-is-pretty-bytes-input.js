import test from 'ava';
import {isPrettyBytesInput} from './index.js';

test('accepts valid input', t => {
	t.true(isPrettyBytesInput(0));
	t.true(isPrettyBytesInput(1337));
	t.true(isPrettyBytesInput(-42));
	t.true(isPrettyBytesInput(5n));
	t.true(isPrettyBytesInput(0n));
});

test('rejects invalid input without throwing', t => {
	t.false(isPrettyBytesInput(Number.NaN));
	t.false(isPrettyBytesInput(Number.POSITIVE_INFINITY));
	t.false(isPrettyBytesInput(Number.NEGATIVE_INFINITY));
	t.false(isPrettyBytesInput('5'));
	t.false(isPrettyBytesInput(null));
	t.false(isPrettyBytesInput(undefined));
	t.false(isPrettyBytesInput(true));
	t.false(isPrettyBytesInput({}));
	t.false(isPrettyBytesInput([]));
});
