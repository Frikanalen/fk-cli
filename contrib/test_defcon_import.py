from __future__ import annotations

import importlib.util
import os
import sys
import unittest


CONTRIB = os.path.dirname(os.path.realpath(__file__))
sys.path.insert(0, CONTRIB)
spec = importlib.util.spec_from_file_location(
    "defcon_import", os.path.join(CONTRIB, "defcon-import.py"))
defcon = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(defcon)


def entry(name, url, is_dir=False, size=""):
    return defcon.DirectoryEntry(name, url, is_dir, size=size)


class ListingParserTests(unittest.TestCase):
    def test_extracts_only_archive_rows(self):
        document = """
            <a href="/elsewhere">navigation</a>
            <table id="list"><tbody>
              <tr><td class="link"><a href="../">Parent directory/</a></td>
                  <td class="size">-</td><td class="date">-</td></tr>
              <tr><td class="link"><a href="Talks%20%26%20video/"
                  title="Talks &amp; video">Talks &amp; video/</a></td>
                  <td class="size">-</td><td class="date">2026 Aug 1</td></tr>
              <tr><td class="link"><a href="A%20talk.mp4">A talk.mp4</a></td>
                  <td class="size">42.1 MiB</td><td class="date">2026 Sep 1</td></tr>
            </tbody></table>
        """
        rows = defcon.parse_listing(document, "https://example.test/DEF%20CON%2034/")
        self.assertEqual([row.name for row in rows], ["Talks & video", "A talk.mp4"])
        self.assertTrue(rows[0].is_dir)
        self.assertFalse(rows[1].is_dir)
        self.assertEqual(rows[1].size, "42.1 MiB")
        self.assertEqual(rows[1].url,
                         "https://example.test/DEF%20CON%2034/A%20talk.mp4")

    def test_recognizes_an_empty_listing(self):
        parser = defcon._parse_listing(
            '<table id="list"><tr><td class="link"><a href="../">Parent</a>'
            "</td></tr></table>", "https://example.test/empty/")
        self.assertTrue(parser.found_list)
        self.assertEqual(parser.entries, [])


class MetadataTests(unittest.TestCase):
    def video(self, number, directory, filename):
        conference = defcon.Conference(
            f"DEF CON {number}", f"https://example.test/DEF%20CON%20{number}/")
        url = conference.url + directory + filename.replace(" ", "%20")
        return defcon.Video(conference, entry(filename, url, size="20 MiB"), 1)

    def test_old_main_stage_order_is_speaker_then_title(self):
        video = self.video(30, "DEF%20CON%2030%20video%20and%20slides/",
                           "DEF CON 30 - Alice - A Talk - With a Subtitle.mp4")
        self.assertEqual(video.title, "A Talk - With a Subtitle")
        self.assertEqual(video.speakers, "Alice")
        self.assertEqual(video.section, "")

    def test_new_main_stage_order_is_title_then_speaker(self):
        video = self.video(33, "DEF%20CON%2033%20video%20and%20slides/",
                           "DEF CON 33 - A Talk - With a Subtitle - Alice.mp4")
        self.assertEqual(video.title, "A Talk - With a Subtitle")
        self.assertEqual(video.speakers, "Alice")

    def test_village_order_includes_a_leading_section(self):
        video = self.video(34, "DEF%20CON%2034%20villages%20and%20creators/",
                           "DEF CON 34 - Voting Village - Alice - A Talk.mp4")
        self.assertEqual(video.title, "A Talk")
        self.assertEqual(video.speakers, "Alice")
        self.assertEqual(video.section, "villages and creators")


class FakeArchive(defcon.Archive):
    def __init__(self):
        super().__init__("https://example.test/")
        root = self.base
        event = root + "DEF%20CON%2033/"
        videos = event + "DEF%20CON%2033%20video%20and%20slides/"
        captions = videos + "captions/"
        pictures = event + "DEF%20CON%2033%20pictures/"
        self.fixture = {
            root: [entry("DEF CON 33", event, True),
                   entry("DEF CON rss", root + "rss/", True)],
            event: [entry("DEF CON 33 video and slides", videos, True),
                    entry("DEF CON 33 pictures", pictures, True),
                    entry("Trailer.mp4", event + "Trailer.mp4", size="2 MiB")],
            videos: [entry("captions", captions, True),
                     entry("A Talk - Alice.mp4", videos + "A%20Talk%20-%20Alice.mp4",
                           size="40 MiB")],
            captions: [],
            pictures: [entry("Behind the scenes.mp4",
                             pictures + "Behind%20the%20scenes.mp4")],
        }

    def listing(self, url):
        return self.fixture[url]


class ArchiveTests(unittest.TestCase):
    def test_resolves_event_aliases(self):
        archive = FakeArchive()
        conference = archive.conference("defcon33")
        self.assertEqual(conference.ref, "dc33")
        self.assertIs(archive.conference("33"), conference)
        self.assertIs(archive.conference("https://example.test/DEF%20CON%2033/"),
                      conference)

    def test_default_walk_uses_recording_branches_and_root_files(self):
        archive = FakeArchive()
        videos = archive.videos(archive.conference("dc33"))
        self.assertEqual([video.name for video in videos],
                         ["A Talk - Alice.mp4", "Trailer.mp4"])
        self.assertEqual([video.ref for video in videos], ["dc33#1", "dc33#2"])
        self.assertEqual(archive.video("dc33#1").title, "A Talk")

    def test_exhaustive_walk_includes_other_branches(self):
        archive = FakeArchive()
        videos = archive.videos(archive.conference("dc33"), all_directories=True)
        self.assertEqual(len(videos), 3)
        self.assertTrue(any(video.name == "Behind the scenes.mp4" for video in videos))

    def test_item_credits_source(self):
        archive = FakeArchive()
        video = archive.video("dc33#1")
        item = defcon.to_item(video, video.ref)
        self.assertEqual(item.title, "A Talk")
        self.assertIn("Med: Alice", item.description)
        self.assertIn(video.url, item.description)
        self.assertEqual(item.language, "")


if __name__ == "__main__":
    unittest.main()
