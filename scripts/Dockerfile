FROM python:latest

WORKDIR /app

RUN apt-get update && apt-get install libportaudio2 -y

COPY listener.py requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

CMD ["python", "./listener.py"]