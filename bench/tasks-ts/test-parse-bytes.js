import test from 'ava';
import {parseBytes} from './index.js';

test('parses default-format output back into a byte count', t => {
	t.is(parseBytes('100 B'), 100);
	t.is(parseBytes('10 kB'), 10000);
	t.is(parseBytes('1 MB'), 1000000);
	t.is(parseBytes('0 B'), 0);
});

test('parses signed and space-less variants', t => {
	t.is(parseBytes('-1 kB'), -1000);
	t.is(parseBytes('+1 kB'), 1000);
	t.is(parseBytes('1kB'), 1000);
});

test('returns null for unparseable input', t => {
	t.is(parseBytes('nonsense'), null);
	t.is(parseBytes('5 XB'), null);
	t.is(parseBytes(''), null);
	t.is(parseBytes(null), null);
	t.is(parseBytes(123), null);
});
