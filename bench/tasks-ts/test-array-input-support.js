import test from 'ava';
import prettyBytes from './index.js';

test('formats each element of an array', t => {
	t.deepEqual(prettyBytes([100, 1337, 999]), ['100 B', '1.34 kB', '999 B']);
});

test('empty array returns empty array', t => {
	t.deepEqual(prettyBytes([]), []);
});

test('array input respects options', t => {
	t.deepEqual(prettyBytes([1000, 1024], {binary: true}), ['1000 B', '1 KiB']);
});

test('array input still validates each element', t => {
	t.throws(() => {
		prettyBytes([100, Number.NaN]);
	});
});

test('does not mutate the input array', t => {
	const input = [100, 1337];
	prettyBytes(input);
	t.deepEqual(input, [100, 1337]);
});

test('scalar input is unaffected', t => {
	t.is(prettyBytes(1337), '1.34 kB');
});
