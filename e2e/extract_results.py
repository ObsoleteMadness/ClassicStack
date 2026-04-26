import machfs
import sys

def extract_results(dsk_path):
    vol = machfs.Volume()
    with open(dsk_path, 'rb') as f:
        vol.read(f.read())

    try:
        results_file = vol[b'results.txt']
        print(results_file.data.decode('macroman'))
    except KeyError:
        print("results.txt not found in disk image")
        sys.exit(1)
    except Exception as e:
        print(f"Error extracting results: {e}")
        sys.exit(1)

if __name__ == '__main__':
    if len(sys.argv) != 2:
        print("Usage: extract_results.py <input_dsk>")
        sys.exit(1)

    dsk_path = sys.argv[1]
    extract_results(dsk_path)
