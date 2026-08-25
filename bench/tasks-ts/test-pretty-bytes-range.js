import test from 'ava';
import {prettyBytesRange} from './index.js';

test('formats a range using the unit chosen for the larger value', t => {
	t.is(prettyBytesRange(500, 1500), '0.5–1.5 kB');
	t.is(prettyBytesRange(1000, 10000), '1–10 kB');
	t.is(prettyBytesRange(999, 1000), '0.999–1 kB');
});

test('equal bounds still return range form', t => {
	t.is(prettyBytesRange(0, 0), '0–0 B');
	t.is(prettyBytesRange(1500, 1500), '1.5–1.5 kB');
});

test('throws when min is greater than max', t => {
	t.throws(() => {
		prettyBytesRange(1500, 500);
	});
});

test('throws for an invalid bound', t => {
	t.throws(() => {
		prettyBytesRange(Number.NaN, 500);
	});
	t.throws(() => {
		prettyBytesRange(0, Number.POSITIVE_INFINITY);
	});
});
