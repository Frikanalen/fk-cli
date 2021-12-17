from tqdm import tqdm
from tusclient import client
import requests
import os
import time
import json

UPLOAD_CHUNK_SIZE = 1024 * 1024
FK_API = 'http://localhost:8000'
MEDIA_API = 'http://localhost:8001'
FK_API_KEY = '1234'

SESSION_COOKIE = 'fk:session'
CSRF_COOKIE = 'fk:csrf'


class FrikanalenSession:
    def __init__(self, email, password):
        session_req = requests.post(
            f'{FK_API}/auth/login',
            {'email': email, 'password': password}
        )

        self.session = session_req.cookies[SESSION_COOKIE]
        self.csrf = session_req.cookies[CSRF_COOKIE]

    def get_frikanalen_tus_client(self):
        def _encode_cookies(cookies: dict) -> str:
            return '; '.join(['='.join(cookie) for cookie in cookies.items()])

        tus_cookies = _encode_cookies({
            SESSION_COOKIE: self.session,
            CSRF_COOKIE: self.csrf
        })

        tus_headers = {
            'Cookie': tus_cookies,
            'X-CSRF-Token': self.csrf,
        }

        return client.TusClient(f'{MEDIA_API}/upload/video/', tus_headers)

    def get_frikanalen_tus_uploader(self, file_path: str):
        file_name = os.path.basename(file_path)
        file_stream = open(file_path, 'rb')
        tus_client = self.get_frikanalen_tus_client()
        return tus_client.uploader(file_stream=file_stream,
                                   chunk_size=UPLOAD_CHUNK_SIZE,
                                   metadata={'fileName': file_name})

    def upload_file(self, file_path: str):
        tus_uploader = self.get_frikanalen_tus_uploader(file_path)
        file_size = os.stat(file_path).st_size
        if(file_size == 0):
            raise Exception("wat")
        progress_bar = tqdm(total=file_size, unit_scale=True, unit='B')

        while tus_uploader.offset < file_size:
            tus_uploader.upload_chunk()
            progress_bar.update(UPLOAD_CHUNK_SIZE)

        progress_bar.close()

        last_response = tus_uploader.request
        if last_response.status_code == 200:
            return json.loads(last_response.response_content)['id']
        else:
            raise Exception('upload failed')
