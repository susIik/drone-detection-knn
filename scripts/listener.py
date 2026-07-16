from time import sleep
import io
import requests
import base64
import sounddevice as sd
from scipy.io.wavfile import write, read


def set_audio_devices():
  out_name = "DigiAMP"
  in_name = "Shure MVX2U"
  out_index = None
  in_index = None

  for i, device in enumerate(sd.query_devices()):
    if in_name.lower() in device["name"].lower():
      in_index = i
    elif out_name.lower() in device["name"].lower():
      out_index = i

  sd.default.device = in_index, out_index # HDMI -> 1, 2; No HDMI -> 0, 1
  print(sd.query_devices(), flush= True)


def rec_audio(fs, seconds):
  myrec = sd.rec(int(seconds * fs), samplerate=fs, channels=1, dtype="int16")
  sd.wait()  # Wait until recording is finished

  wav_buffer = io.BytesIO() # save file to memory as bytes
  write(wav_buffer, fs, myrec)

  audio_b64 = base64.b64encode(wav_buffer.getvalue()).decode("utf-8") # convert to base64 vaw

  payload = {
      "audio": audio_b64,
      "channels": 1,
      "sampleRate": fs,
      "sampleSize": 16,
      "duration": seconds,
      #"latitude": None,
      #"longitude": None
  }

  return payload

def listen():
  fs = 44100  # Sample rate
  seconds = 5  # Duration of recording
  fs, audio = read("danger.wav")
  backend_url = "http://backend:5001"

  while True:
    payload = rec_audio(fs, seconds)
    response = requests.post(
        backend_url + "/api/audio/classify",
        json=payload
    )

    if response.json()["isDrone"]:
      sd.play(audio, fs)
      sd.wait()

    
    print(response.status_code)
    print(response.json()["isDrone"])
    print(response.json()["predictions"], flush = True)
    sleep(1)

if __name__ == "__main__":
  set_audio_devices()
  listen()