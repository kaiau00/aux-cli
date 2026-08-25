import test from 'ava';
import prettyBytes from './index.js';

test('unitOnly returns just the unit', t => {
	t.is(prettyBytes(1337, {unitOnly: true}), 'kB');
	t.is(prettyBytes(999, {unitOnly: true}), 'B');
	t.is(prettyBytes(0, {unitOnly: true}), 'B');
	t.is(prettyBytes(1337, {unitOnly: true, bits: true}), 'kbit');
	t.is(prettyBytes(1024, {unitOnly: true, binary: true}), 'KiB');
});

test('unitOnly still validates the number', t => {
	t.throws(() => {
		prettyBytes(Number.NaN, {unitOnly: true});
	});
});

test('unitOnly ignores number formatting options', t => {
	t.is(prettyBytes(1337, {unitOnly: true, signed: true, fixedWidth: 20}), 'kB');
});
