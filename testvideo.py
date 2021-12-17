import ffmpeg
import os
import tempfile
from session import FrikanalenSession


def get_font():
    try:
        font_path = '/usr/share/fonts/truetype/inconsolata/Inconsolata.otf'
        os.stat(font_path)
        return(font_path)
    except FileNotFoundError:
        return None


def make_testvideo(duration: float, text: str, filepath: str):
    textoptions = {
        "box": True,
        "fontsize": 72,
        "boxborderw": 20,
        "fontfile": get_font(),
        "fontcolor": "white",
        "boxcolor": "black",
        "line_spacing": 20,
        "x": "(w-text_w)/2",
        "y": "(h-text_h-line_h)/2",
        "expansion": "normal",
        "escape_text": False,
    }
    (
        ffmpeg
        .input(f'testsrc=duration={duration}:size=1280x720:rate=50', format="lavfi")
        .drawtext(text + '\n%{ pts:hms }', **textoptions)
        .output(filepath)
        .overwrite_output()
        .run()
    )


def upload_testvideo(session: FrikanalenSession):

    with tempfile.TemporaryDirectory(prefix="fk-cli") as tmpdirname:
        test_file = os.path.join(tmpdirname, "testfile.mxf")
        make_testvideo(1, "hello\nheloasdfasdfdf", test_file)
        return session.upload_file(test_file)
